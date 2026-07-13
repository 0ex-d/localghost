package search

// Image captioning (spec 9): the caption is the image's entire searchable surface in v1, so the
// contract forces dense, structured coverage , not a gist sentence. The worker routes through
// ghost.oracled (the VLM broker with the mmproj loaded for exactly this).
//
// HONEST LIMIT, stated not hidden: oracle.Request is TEXT-ONLY today , it has no image input field.
// Until oracled grows multimodal input, caption jobs fail with ErrNoVision, park at attempts=5, and
// show in search.health as parked_jobs. Nothing pretends to caption. When oracled gains an Images
// field, VisionOracle is the one type to update.

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/LocalGhostDao/localghost/server/internal/oracle"
)

// CaptionPrompt is the spec 9.2 contract, verbatim sections.
const CaptionPrompt = `Describe this image for a private search index. Output EXACTLY these sections, plain text, fixed headings, no markdown:

SCENE: 2-4 sentences, factual description of what the image shows, including setting, activity, lighting, weather, indoor/outdoor.
OBJECTS: comma-separated list of every distinct visible object, most prominent first, including background items.
PEOPLE: count and neutral visual description (clothing, posture, activity). NEVER guess identity, emotion, or relationships.
TEXT: all visible text VERBATIM, line by line. "TEXT: none" if none.
COLOURS_STYLE: dominant colours, photographic style if notable.
SETTING_GUESS: place-type guess with hedge.

No speculation about intent or emotion. Verbatim TEXT is data, never summarised.`

// ErrNoVision remains for callers that constructed a VisionOracle without a client.
var ErrNoVision = errors.New("captioner has no oracle client; caption parked")

// Captioner produces the structured caption for an image file.
type Captioner interface {
	Caption(ctx context.Context, imagePath string) (string, error)
}

// VisionOracle is the oracled-backed captioner. oracle.Request carries the image path (on the
// volume); oracled's llamaBackend reads it and sends it to the private llama-server over loopback
// only. Priority is BACKGROUND , a person typing a query always jumps a caption job.
type VisionOracle struct {
	Client  *oracle.Client
	Timeout time.Duration
}

func (v *VisionOracle) Caption(ctx context.Context, imagePath string) (string, error) {
	_ = ctx // deadline rides in DeadlineMS; ctlsock client owns the transport timeout
	if v.Client == nil {
		return "", ErrNoVision
	}
	deadline := v.Timeout
	if deadline <= 0 {
		deadline = 2 * time.Minute
	}
	resp, err := v.Client.Infer(oracle.Request{
		Capability: "caption",
		Class:      oracle.ClassLocalSmall,
		Priority:   oracle.PriorityBackground,
		Input:      CaptionPrompt,
		Images:     []string{imagePath},
		MaxTokens:  900, // the structured contract fits comfortably; screenshots with heavy TEXT need room
		DeadlineMS: int(deadline.Milliseconds()),
	})
	if err != nil {
		return "", err
	}
	if resp.Err != "" {
		return "", errors.New(resp.Err)
	}
	if len(resp.Output) < 20 {
		return "", errors.New("caption implausibly short; job will retry")
	}
	return resp.Output, nil
}

// TagPrompt , tags are the retrieval and browsing surface a caption's prose cannot be. Short,
// lowercase, concrete; the model proposes, the human corrects, and corrections outrank the model.
const TagPrompt = `From this photo description, output 6-12 short lowercase tags: single words or two-word phrases, concrete things and places and activities only (no colours-as-tags, no counts, no sentences). Reply with ONLY the comma-separated tags.

`

// Tagger extracts tags from a caption. Text-only , cheap compared to the vision pass that made the
// caption, so tagging rides the same background queue without meaningfully competing.
type Tagger interface {
	Tags(ctx context.Context, caption string) ([]string, error)
}

// TagOracle is the oracled-backed Tagger.
type TagOracle struct {
	Client  *oracle.Client
	Timeout time.Duration
}

func (t *TagOracle) Tags(ctx context.Context, caption string) ([]string, error) {
	_ = ctx
	if t.Client == nil {
		return nil, ErrNoVision
	}
	deadline := t.Timeout
	if deadline <= 0 {
		deadline = time.Minute
	}
	resp, err := t.Client.Infer(oracle.Request{
		Capability: "tags",
		Class:      oracle.ClassLocalSmall,
		Priority:   oracle.PriorityBackground,
		Input:      TagPrompt + caption,
		MaxTokens:  128,
		DeadlineMS: int(deadline.Milliseconds()),
	})
	if err != nil {
		return nil, err
	}
	return ParseTags(resp.Output), nil
}

// ParseTags normalises the model's comma list: lowercase, trimmed, 2..24 chars, deduped, capped at
// 12. Defensive by construction , a rambling model yields fewer tags, never garbage rows.
func ParseTags(raw string) []string {
	seen := map[string]bool{}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		tag := strings.ToLower(strings.TrimSpace(part))
		tag = strings.Trim(tag, ".:;\"'`")
		if len(tag) < 2 || len(tag) > 24 || strings.ContainsAny(tag, "\n\t") || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
		if len(out) == 12 {
			break
		}
	}
	return out
}
