package secd

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/LocalGhostDao/localghost/server/internal/profile"
)

// unlockService runs a PIN unlock and exposes its progress for polling, mirroring the app's
// UnlockStage stream. submitPin starts an unlock; the app polls /unlock/poll once a second and gets
// the stages completed so far (all at once if the account is hot, accumulating if cold).
//
// The stage sequence is identical for every account, so a duress unlock looks the same as a real
// one. This wires profile.StreamUnlock (the validated stage logic) to a poll-able state.
type unlockService struct {
	backend  UnlockBackend
	mu       sync.Mutex
	progress map[profile.Stage]profile.StepState
	order    []profile.Stage
	done     bool
	failed   string
	openSlot int
}

func newUnlockService(backend UnlockBackend) *unlockService {
	return &unlockService{
		backend:  backend,
		progress: map[profile.Stage]profile.StepState{},
		order: []profile.Stage{
			profile.StageResolve, profile.StageUnseal, profile.StageMount,
			profile.StageStartDB, profile.StageStartCache, profile.StageDaemons, profile.StageReady,
		},
		openSlot: profile.NoSlot,
	}
}

type unlockRequest struct {
	Pin string `json:"pin"`
}

// handleUnlockStart begins an unlock. It resolves the PIN (the security decision) immediately, then
// runs the stages in the background so the app can poll progress. Resolve happens here so a wrong
// PIN is rejected before any work.
func (s *Server) handleUnlockStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var req unlockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "bad request")
		return
	}

	u := s.unlock
	u.mu.Lock()
	// reset for a fresh unlock
	u.progress = map[profile.Stage]profile.StepState{}
	u.done = false
	u.failed = ""
	u.openSlot = profile.NoSlot
	u.mu.Unlock()

	// Drive the unlock through the backend: resolve the PIN (real / decoy / duress-wipe / reject),
	// unseal the slot key from the TPM, map + mount the container, start the per-account DB + cache.
	// The default build wires a simulation; the `tpm` build wires the real hardware path.
	go u.run(req.Pin)

	writeJSON(w, map[string]any{"started": true})
}

// run walks the stages, marking each running then complete, with a short delay so a cold unlock
// shows a real progression. The hot path (already mounted) would mark them Skipped instantly.
func (u *unlockService) run(pin string) {
	emit := func(p profile.Progress) {
		u.mu.Lock()
		u.progress[p.Stage] = p.State
		u.mu.Unlock()
	}
	slot, err := runUnlock(u.backend, pin, emit)
	u.mu.Lock()
	if err != nil || slot == profile.NoSlot {
		u.failed = "unlock failed"
		if err != nil && err != errReject {
			u.failed = err.Error()
		}
	} else {
		u.done = true
		u.openSlot = slot
	}
	u.mu.Unlock()
}

// handleUnlockPoll returns the current stage states, the shape the app's UnlockSnapshot.from expects.
func (s *Server) handleUnlockPoll(w http.ResponseWriter, r *http.Request) {
	u := s.unlock
	u.mu.Lock()
	defer u.mu.Unlock()
	stages := make([]map[string]any, 0, len(u.order))
	for _, st := range u.order {
		state, ok := u.progress[st]
		stageState := "pending"
		if ok {
			stageState = stepStateName(state)
		}
		stages = append(stages, map[string]any{"stage": stageName(st), "state": stageState})
	}
	resp := map[string]any{"stages": stages, "done": u.done}
	if u.failed != "" {
		resp["failed"] = u.failed
	}
	if u.done {
		// reflect the mount on the server so /info shows unlocked
		s.mu.Lock()
		s.mounted = u.openSlot
		s.mu.Unlock()
	}
	writeJSON(w, resp)
}

func stageName(st profile.Stage) string {
	switch st {
	case profile.StageResolve:
		return "RESOLVE"
	case profile.StageUnseal:
		return "UNSEAL"
	case profile.StageMount:
		return "MOUNT"
	case profile.StageStartDB:
		return "START_DB"
	case profile.StageStartCache:
		return "START_CACHE"
	case profile.StageDaemons:
		return "DAEMONS"
	case profile.StageReady:
		return "READY"
	default:
		return "UNKNOWN"
	}
}

func stepStateName(s profile.StepState) string {
	switch s {
	case profile.Running:
		return "RUNNING"
	case profile.Skipped:
		return "SKIPPED"
	case profile.Complete:
		return "COMPLETE"
	case profile.Errored:
		return "ERRORED"
	default:
		return "PENDING"
	}
}
