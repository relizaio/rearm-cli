package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/relizaio/rearm-client-go/sessiontest"
	"github.com/spf13/viper"
)

// S401-1: several real processes on one credentials file, as on a host with a wait per board seat
// and the usage hooks. Each child is this test binary, running the CLI's own client path.

const processRounds = 4

func waitForFile(path string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// TestCredentialsFileChildProcess is the child: it loads the credentials file once, as a rearm
// process does at start, then makes three requests per round when the parent says go.
func TestCredentialsFileChildProcess(t *testing.T) {
	if os.Getenv("REARM_TEST_CREDSTORE_CHILD") != "1" {
		t.Skip("child of TestProcessesSharingOneCredentialsFile")
	}
	dir, id := os.Getenv("REARM_TEST_CREDSTORE_DIR"), os.Getenv("REARM_TEST_CREDSTORE_ID")
	cfgFile, rearmUri, apiClient = os.Getenv("REARM_TEST_CREDSTORE_CFG"), os.Getenv("REARM_TEST_CREDSTORE_URI"), nil
	v := viper.New()
	v.SetConfigFile(cfgFile)
	v.SetConfigType(configType)
	if err := v.ReadInConfig(); err != nil {
		t.Fatal(err)
	}
	loadSessionConfig(v)
	for r := 0; r < processRounds; r++ {
		if !waitForFile(filepath.Join(dir, fmt.Sprintf("go-%d", r)), 60*time.Second) {
			t.Fatalf("no go for round %d", r)
		}
		result := ""
		for i := 0; i < 3 && result == ""; i++ {
			if _, err := sendGraphQLRequest("query Ping { __typename }", nil); err != nil {
				result = err.Error()
			}
		}
		if !sessionStoreInUse {
			result = "the child did not renew through the store"
		}
		_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("done-%d-%s", r, id)), []byte(result), 0o600)
	}
}

func TestProcessesSharingOneCredentialsFile(t *testing.T) {
	if os.Getenv("REARM_TEST_CREDSTORE_CHILD") == "1" {
		t.Skip("running as a child")
	}
	srv := withSessionFile(t, sessiontest.Options{})
	// round 0 is the hour boundary seen from the file: every process starts with the token due
	on, _ := credentialsStore{}.Load()
	on.AccessTokenExpiry = time.Now().Add(-time.Second)
	if err := (credentialsStore{}).Save(on); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	const children = 4
	var procs []*exec.Cmd
	for i := 0; i < children; i++ {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCredentialsFileChildProcess$", "-test.count=1")
		cmd.Env = append(os.Environ(), "REARM_TEST_CREDSTORE_CHILD=1", "REARM_TEST_CREDSTORE_DIR="+dir, "REARM_TEST_CREDSTORE_ID="+strconv.Itoa(i), "REARM_TEST_CREDSTORE_CFG="+cfgFile, "REARM_TEST_CREDSTORE_URI="+srv.URL)
		out, err := os.Create(filepath.Join(dir, "child-"+strconv.Itoa(i)+".log"))
		if err != nil {
			t.Fatal(err)
		}
		defer out.Close()
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		procs = append(procs, cmd)
	}
	defer func() {
		for _, p := range procs {
			_ = p.Process.Kill()
		}
	}()
	for r := 0; r < processRounds; r++ {
		if r > 0 {
			// another hour: every token on file and in every process has expired server-side, and each
			// process still believes in its own
			srv.Advance(time.Hour + time.Minute)
		}
		before := srv.Counts().Refreshes
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("go-%d", r)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < children; i++ {
			done := filepath.Join(dir, fmt.Sprintf("done-%d-%d", r, i))
			if !waitForFile(done, 60*time.Second) {
				log, _ := os.ReadFile(filepath.Join(dir, "child-"+strconv.Itoa(i)+".log"))
				t.Fatalf("round %d: child %d did not finish:\n%s", r, i, log)
			}
			if res, _ := os.ReadFile(done); len(res) > 0 {
				t.Fatalf("round %d: child %d: %s", r, i, res)
			}
		}
		if got := srv.Counts().Refreshes - before; got != 1 {
			t.Fatalf("round %d: one refresh for %d processes, got %d (%+v)", r, children, got, srv.Counts())
		}
	}
	for i, p := range procs {
		if err := p.Wait(); err != nil {
			log, _ := os.ReadFile(filepath.Join(dir, "child-"+strconv.Itoa(i)+".log"))
			t.Fatalf("child %d: %v\n%s", i, err, log)
		}
	}
	procs = nil
	noSessionHarm(t, srv)
	on, _ = credentialsStore{}.Load()
	if !srv.IsCurrent(on.RefreshToken) {
		t.Fatal("the file ends with the server's current refresh token")
	}
}
