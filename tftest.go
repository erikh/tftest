package tftest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"sync"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

const (
	tfstateFilename = "terraform.tfstate"
	planFilename    = "plan.tf"
)

// TerraformPluginCacheDir is where the plugins we download are kept. See
// InitCache() (called on boot) and CleanCache(). You can also override it
// before calling anything to move it if it's a problem.
var TerraformPluginCacheDir = "/tmp/tftest/plugin_cache"

func init() {
	InitCache()
}

// InitCache creates the cache directory. It does not care about errors.
func InitCache() {
	os.MkdirAll(TerraformPluginCacheDir, 0700)
}

// CleanCache cleans our plugin cache by removing it. Put this in TestMain in
// your tests.
func CleanCache() {
	os.RemoveAll(TerraformPluginCacheDir)
}

// State is the parsed state from terraform apply actions.
type State map[string]interface{}

// Harness is the entrypoint into the tftest system
type Harness struct {
	terraformPath string
	testingT      *testing.T
	state         State // will be nil until after apply
	tfstatePath   string
	plandir       string
	commandLock   sync.Mutex
	commandCancel context.CancelFunc
	sigCancel     context.CancelFunc
}

// New creates a new tftest harness
func New(t *testing.T) *Harness {
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

	return &h
}

func (h *Harness) t() *testing.T {
	return h.testingT
}

func (h *Harness) tf(plandir string, command ...string) error {
	h.commandLock.Lock()
	defer h.commandLock.Unlock()
	if h.commandCancel != nil {
		h.commandCancel()
	}

	var ctx context.Context
	ctx, h.commandCancel = context.WithCancel(context.Background())

	// FIXME stream output with pipes
	cmd := exec.CommandContext(ctx, h.terraformPath, command...)
	cmd.Dir = plandir
	cmd.Env = append(os.Environ(), fmt.Sprintf("TF_PLUGIN_CACHE_DIR=%s", TerraformPluginCacheDir))

	out, err := cmd.CombinedOutput()
	h.t().Log(string(out)) // basically, always log since people can turn it off by not supplying -v
	return err
}

// Apply the harness and resources with terraform. Apply additionally sets up a
// Cleanup hook to teardown the environment when the test tears down, and
// parses the state (see State()).
//
// The cleanup hook is not installed when NO_CLEANUP=1 is set in the environment.
func (h *Harness) Apply(planfile string) {
	h.plandir = h.t().TempDir() // out dir for state; will be reaped automatically

	source, err := os.Open(planfile)
	if err != nil {
		h.t().Fatalf("Could not open plan file: %v", err)
	}
	defer source.Close()

	target, err := os.Create(path.Join(h.plandir, "plan.tf"))
	if err != nil {
		h.t().Fatalf("Could not open target file for writing: %v", err)
	}
	defer target.Close()

	if _, err := io.Copy(target, source); err != nil {
		h.t().Fatalf("Could not copy source to target: %v", err)
	}

	if os.Getenv("NO_CLEANUP") == "" {
		h.t().Cleanup(h.Destroy)
	}

	if err := h.tf(h.plandir, fmt.Sprintf("-chdir=%s", h.plandir), "init"); err != nil {
		h.t().Fatalf("while initializing terraform: %v", err)
	}

	if err := h.tf(h.plandir, "apply", "-auto-approve"); err != nil {
		h.t().Fatalf("while applying terraform: %v", err)
	}

	h.readState()
}

func (h *Harness) readState() {
	f, err := os.Open(path.Join(h.plandir, tfstateFilename))
	if err != nil {
		h.t().Fatalf("while reading tfstate: %v", err)
	}
	defer f.Close()

	h.state = State{}

	if err := json.NewDecoder(f).Decode(&h.state); err != nil {
		h.t().Fatalf("while decoding tfstate JSON: %v", err)
	}
}

// Refresh applies terraform update to an existing tftest plandir.
func (h *Harness) Refresh() {
	if h.plandir == "" {
		h.t().Fatal("run Apply() first!")
	}

	if err := h.tf(h.plandir, "refresh"); err != nil {
		h.t().Fatalf("while refresh terraform: %v", err)
	}

	h.readState()
}

// Destroy the harness and resources with terraform. Discard this struct after calling this method.
func (h *Harness) Destroy() {
	if err := h.tf(h.plandir, "destroy", "-auto-approve"); err != nil {
		h.t().Fatalf("while destroying resources with terraform: %v", err)
	}
}

// State corresponds to the terraform state. This is ingested on each "apply"
// step, and will be nil until apply is called the first time.
func (h *Harness) State() State {
	return h.state
}

// PlanDir returns the path to the plan and state, which may be useful in
// certain failure situations.
func (h *Harness) PlanDir() string {
	return h.plandir
}

// HandleSignals handles SIGINT and SIGTERM to ensure that containers get
// cleaned up. It is expected that no other signal handler will be installed
// afterwards. If the forward argument is true, it will forward the signal back
// to its own process after deregistering itself as the signal handler,
// allowing your test suite to exit gracefully. Set it to false to stay out of
// your way.
//
// taken from https://github.com/erikh/duct
func (h *Harness) HandleSignals(forward bool) {
	ctx, cancel := context.WithCancel(context.Background())
	h.sigCancel = cancel
	sigChan := make(chan os.Signal, 2)

	go func() {
		select {
		case sig := <-sigChan:
			log.Println("Signalled; will destroy terraform now")
			h.Destroy()
			signal.Stop(sigChan) // stop letting us get notified
			if forward {
				unix.Kill(os.Getpid(), sig.(syscall.Signal))
			}
		case <-ctx.Done():
			signal.Stop(sigChan)
		}
	}()

	signal.Notify(sigChan, unix.SIGINT, unix.SIGTERM)
}
