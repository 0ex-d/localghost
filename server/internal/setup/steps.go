package setup

import (
	"errors"
	"fmt"
)

// ghost.secd setup is a lot: it provisions the box's identity, the privileged ghost user, the device
// CA, nginx, optional DNS, the account PINs, and the TPM seal. It is written as an ordered list of
// idempotent steps so it can be re-run safely (each step checks whether it is already done before
// doing it) and so a failure stops at a named place with a clear message rather than half-applying.
//
// Run as root: setup creates the unprivileged ghost user and from then on the daemons run as ghost,
// but the initial provisioning (user creation, nginx, systemd, TPM owner ops) needs root.

// StepStatus is the outcome of one step.
type StepStatus int

const (
	Pending StepStatus = iota
	AlreadyDone
	Done
	Failed
)

func (s StepStatus) String() string {
	switch s {
	case AlreadyDone:
		return "already done"
	case Done:
		return "done"
	case Failed:
		return "failed"
	default:
		return "pending"
	}
}

// Step is one unit of setup. Check reports whether it is already satisfied (so a re-run skips it);
// Describe says what Do WOULD do (printed in the dry run, before anything is touched); Do performs
// it. Destructive marks steps that erase data (partitioning, formatting) so they are hard-guarded
// from ever running outside an explicit Apply.
type Step struct {
	Name        string
	Destructive bool
	Check       func() (bool, error)  // true => already done, skip Do
	Describe    func() (string, error) // human line for the dry run: what Do would do
	Do          func() error
}

// Plan is the ordered setup sequence. The order matters and is enforced: identity and the ghost user
// before anything runs as ghost; the device CA before a device cert can be issued; nginx before it
// can be told to reject unverified clients; DNS verified before nginx is pointed at a public name.
type Plan struct {
	steps []Step
}

func NewPlan(steps ...Step) *Plan { return &Plan{steps: steps} }

// Result records what happened to one step.
type Result struct {
	Name   string
	Status StepStatus
	Err    error
}

var (
	ErrStepFailed   = errors.New("setup step failed")
	ErrNginxMissing = errors.New("nginx is not installed; install it and re-run setup")
	ErrTPMUnusable  = errors.New("no usable TPM 2.0; wipe will be best-effort and a short PIN is not safe from offline brute force")
	ErrDryRunDirty  = errors.New("dry run found problems; nothing was applied")
)

// Planned is what the dry run reports for one step, with no side effects.
type Planned struct {
	Name        string
	Destructive bool
	Skip        bool   // already satisfied
	Action      string // what Do would do (from Describe)
	Problem     error  // a precondition failure surfaced by Check
}

// DryRun walks every step WITHOUT touching anything: it runs Check and Describe only, so it can
// print the full plan (including destructive actions like "partition /dev/X, this erases it") and
// surface precondition failures up front. It never calls Do. The operator reads this, then confirms
// before Apply. This is how "print what it will do, and only do it at the very end" becomes a
// structural property rather than a discipline.
func (p *Plan) DryRun() ([]Planned, error) {
	out := make([]Planned, 0, len(p.steps))
	clean := true
	for _, step := range p.steps {
		done, err := step.Check()
		if err != nil {
			out = append(out, Planned{Name: step.Name, Destructive: step.Destructive, Problem: err})
			clean = false
			continue
		}
		if done {
			out = append(out, Planned{Name: step.Name, Destructive: step.Destructive, Skip: true})
			continue
		}
		action := ""
		if step.Describe != nil {
			a, derr := step.Describe()
			if derr != nil {
				out = append(out, Planned{Name: step.Name, Destructive: step.Destructive, Problem: derr})
				clean = false
				continue
			}
			action = a
		}
		out = append(out, Planned{Name: step.Name, Destructive: step.Destructive, Action: action})
	}
	if !clean {
		return out, ErrDryRunDirty
	}
	return out, nil
}

// Apply executes the plan for real, in order, skipping already-done steps and STOPPING at the first
// failure (a half-provisioned box is worse than a clearly-incomplete one). It must be preceded by a
// clean DryRun; pass its result so Apply refuses to run if the dry run was dirty. Destructive steps
// run only here, never in DryRun, so partitioning/formatting cannot happen during a preview.
func (p *Plan) Apply(dryRun []Planned) ([]Result, error) {
	for _, pl := range dryRun {
		if pl.Problem != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrDryRunDirty, pl.Name, pl.Problem)
		}
	}
	results := make([]Result, 0, len(p.steps))
	for _, step := range p.steps {
		done, err := step.Check()
		if err != nil {
			results = append(results, Result{step.Name, Failed, err})
			return results, fmt.Errorf("%w: %s (check): %v", ErrStepFailed, step.Name, err)
		}
		if done {
			results = append(results, Result{step.Name, AlreadyDone, nil})
			continue
		}
		if err := step.Do(); err != nil {
			results = append(results, Result{step.Name, Failed, err})
			return results, fmt.Errorf("%w: %s: %v", ErrStepFailed, step.Name, err)
		}
		results = append(results, Result{step.Name, Done, nil})
	}
	return results, nil
}
