package tftest

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"testing"
)

const tfstateFilename = "tfstate"

// State is the parsed state from terraform apply actions.
type State map[string]interface{}

// Harness is the entrypoint into the tftest system
type Harness struct {
	terraformPath string
	testingT      *testing.T
	state         State // will be nil until after apply
	tfstatePath   string
	plandir       string
}

// New creates a new tftest harness
func New(t *testing.T) Harness {
	var h Harness

	h.terraformPath = os.Getenv("TFTEST_TERRAFORM")

	if h.terraformPath == "" {
		var err error
		h.terraformPath, err = exec.LookPath("terraform")
		if err != nil {
			t.Fatal(err)
		}
	}

	h.testingT = t

	return h
}

func (h Harness) t() *testing.T {
	return h.testingT
}

func (h Harness) tf(plandir string, command ...string) error {
	// FIXME stream output with pipes
	cmd := exec.Command(h.terraformPath, command...)
	cmd.Dir = plandir
	out, err := cmd.CombinedOutput()
	h.t().Log(string(out)) // basically, always log since people can turn it off by not supplying -v
	return err
}

// Apply the harness and resources with terraform. Apply additionally sets up a
// Cleanup hook to teardown the environment when the test tears down, and
// parses the state (see State()).
func (h *Harness) Apply(plandir string) {
	dir := h.t().TempDir() // out dir for state; will be reaped automatically

	h.plandir = plandir
	h.tfstatePath = path.Join(dir, tfstateFilename)

	if err := h.tf(h.plandir, "apply", "-auto-approve", fmt.Sprintf("-state=%s", h.tfstatePath)); err != nil {
		h.t().Fatalf("while applying terraform: %v", err)
	}

	h.t().Cleanup(h.Destroy)

	f, err := os.Open(h.tfstatePath)
	if err != nil {
		h.t().Fatalf("while reading tfstate: %v", err)
	}
	defer f.Close()

	h.state = State{}

	if err := json.NewDecoder(f).Decode(&h.state); err != nil {
		h.t().Fatalf("while decoding tfstate JSON: %v", err)
	}
}

// Destroy the harness and resources with terraform. Discard this struct after calling this method.
func (h Harness) Destroy() {
	if err := h.tf(h.plandir, "destroy", "-auto-approve", fmt.Sprintf("-state=%s", h.tfstatePath)); err != nil {
		h.t().Fatalf("while destroying resources with terraform: %v", err)
	}
}

// State corresponds to the terraform state. This is ingested on each "apply"
// step, and will be nil until apply is called the first time.
func (h Harness) State() State {
	return h.state
}

// StatePath returns the path to the state, which may be useful in certain
// failure situations.
func (h Harness) StatePath() string {
	return h.tfstatePath
}
