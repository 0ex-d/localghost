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
	"fmt"
	"regexp"
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
	Client      *oracle.Client
	Timeout     time.Duration // the model on the GPU (default 2 min)
	SlowTimeout time.Duration // the model on the CPU (default 15 min: image encode + up to 1800 tokens)
	Pace        *Pace         // nil = always the GPU budget
}

func (v *VisionOracle) deadline() time.Duration {
	fast, slow := v.Timeout, v.SlowTimeout
	if fast <= 0 {
		fast = 2 * time.Minute
	}
	if slow <= 0 {
		slow = 15 * time.Minute
	}
	return v.Pace.pick(fast, slow)
}

func (v *VisionOracle) Caption(ctx context.Context, imagePath string) (string, error) {
	_ = ctx // deadline rides in DeadlineMS; ctlsock client owns the transport timeout
	if v.Client == nil {
		return "", ErrNoVision
	}
	deadline := v.deadline()
	resp, err := v.Client.Infer(oracle.Request{
		Capability: "caption",
		Class:      oracle.ClassLocalSmall,
		Priority:   oracle.PriorityBackground,
		Input:      CaptionPrompt,
		Images:     []string{imagePath},
		MaxTokens:  1800, // this model THINKS first; if the template ignores enable_thinking=false,
		// the budget must cover monologue + answer , at ~45 tok/s that is ~40s worst case per
		// caption, acceptable for a background queue and moot once suppression works
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
	// THE SECTIONS ARE THE CONTRACT. A caption without a SCENE section is not a caption , the
	// model's thinking that ran past the token budget, a refusal, prose in a shape nobody parses ,
	// and storing it meant a frame with a "caption" that never described it: the description
	// stayed empty, the tag pass had nothing to read, and every stock-take found it "undescribed",
	// asked for it again, saw a caption in meta, and moved on. Headings the model dresses up
	// ("**SCENE:**", "Scene:", "## SCENE") are normalised; a caption with no SCENE at all fails
	// the job, so it retries and, if the model keeps doing it, parks where the queue line shows it.
	out, ok := NormalizeCaption(resp.Output)
	if !ok {
		return "", errors.New("caption without a SCENE section (the model did not answer in the fixed sections); job will retry")
	}
	return out, nil
}

// captionHeadings are the fixed sections, in the order the prompt asks for them.
var captionHeadings = []string{"SCENE:", "OBJECTS:", "PEOPLE:", "TEXT:", "COLOURS_STYLE:", "SETTING_GUESS:"}

// NormalizeCaption puts the model's headings back into the fixed form (markdown, case and
// spacing stripped: "**Scene:**", "## OBJECTS :", "colours_style:" all become the contract's
// headings, each at the start of its own line) and reports whether a SCENE section is there at
// all. Text before the first heading (a preamble, leaked thinking) is dropped.
func NormalizeCaption(raw string) (string, bool) {
	s := strings.ReplaceAll(raw, "\r", "")
	// COLORS_STYLE and COLOURS STYLE are the same heading to a model that spells
	s = headingRe.ReplaceAllStringFunc(s, func(m string) string {
		name := strings.ToUpper(headingRe.FindStringSubmatch(m)[1])
		name = strings.ReplaceAll(strings.ReplaceAll(name, " ", "_"), "COLORS", "COLOURS")
		return "\n" + name + ": "
	})
	i := strings.Index(s, "SCENE:")
	if i < 0 {
		return "", false
	}
	s = strings.TrimSpace(s[i:])
	if len(captionSection(s, "SCENE:")) < 5 {
		return "", false // a heading with nothing under it describes nothing
	}
	return s, true
}

// headingRe matches a dressed-up heading: optional markdown/hash/bullet, the name in any case with
// space or underscore, optional markdown, the colon, optional markdown after.
var headingRe = regexp.MustCompile(`(?i)(?:^|\n)[ \t]*[#*_\-]*[ \t]*(scene|objects|people|text|colou?rs[ _]style|setting[ _]guess)[ \t]*[*_]*[ \t]*:[ \t]*[*_]*[ \t]*`)

// Tagger extracts tags from a caption. Text-only , cheap compared to the vision pass that made the
// caption, so tagging rides the same background queue without meaningfully competing.
type Tagger interface {
	Tags(ctx context.Context, caption string) ([]Tag, error)
	// Categorize assigns categories to existing bare tags (the backfill); the lexicon is tried
	// first by the caller, so this only sees what needs a model.
	Categorize(ctx context.Context, tags []string) ([]Tag, error)
}

// TagOracle is the oracled-backed Tagger.
type TagOracle struct {
	Client      *oracle.Client
	Timeout     time.Duration // the model on the GPU (default 1 min)
	SlowTimeout time.Duration // the model on the CPU (default 8 min)
	Pace        *Pace
}

func (t *TagOracle) Tags(ctx context.Context, caption string) ([]Tag, error) {
	_ = ctx
	if t.Client == nil {
		return nil, ErrNoVision
	}
	resp, err := t.Client.Infer(oracle.Request{
		Capability: "tags",
		Class:      oracle.ClassLocalSmall,
		Priority:   oracle.PriorityBackground,
		Input:      TagPrompt + caption,
		MaxTokens:  160,
		DeadlineMS: int(t.deadline().Milliseconds()),
	})
	if err != nil {
		return nil, err
	}
	// A failed inference is an error, not "no tags": before this, a tag pass that hit its deadline
	// came back empty and the job completed with the frame untagged, for good.
	if resp.Err != "" {
		return nil, errors.New(resp.Err)
	}
	return ParseTags(resp.Output), nil
}

// Categorize asks the model for the categories of tags that have none, ten at a time. Every tag
// asked about comes back with a category (OtherCategory when the model placed it nowhere); an
// answer with no category:tag pair in it is an error, with the start of the answer in it, so the
// job's failure line in the log shows what the model said.
func (t *TagOracle) Categorize(ctx context.Context, tags []string) ([]Tag, error) {
	_ = ctx
	if t.Client == nil {
		return nil, ErrNoVision
	}
	var out []Tag
	for start := 0; start < len(tags); start += categorizeChunk {
		end := start + categorizeChunk
		if end > len(tags) {
			end = len(tags)
		}
		chunk := tags[start:end]
		resp, err := t.Client.Infer(oracle.Request{
			Capability: "tags",
			Class:      oracle.ClassLocalSmall,
			Priority:   oracle.PriorityBackground,
			Input:      CategorizePrompt + strings.Join(chunk, ", "),
			MaxTokens:  24 * len(chunk),
			DeadlineMS: int(t.deadline().Milliseconds()),
		})
		if err != nil {
			return nil, err
		}
		if resp.Err != "" {
			return nil, errors.New(resp.Err)
		}
		got, ok := AssignCategories(chunk, resp.Output)
		if !ok {
			return nil, fmt.Errorf("the model's answer has no category:tag pair: %q", clipRunes(resp.Output, 160))
		}
		out = append(out, got...)
	}
	return out, nil
}

func (t *TagOracle) deadline() time.Duration {
	fast, slow := t.Timeout, t.SlowTimeout
	if fast <= 0 {
		fast = time.Minute
	}
	if slow <= 0 {
		slow = 8 * time.Minute
	}
	return t.Pace.pick(fast, slow)
}

// clipRunes is s cut to at most n runes, for a log line.
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
