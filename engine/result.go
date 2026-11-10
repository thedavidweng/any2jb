package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/thedavidweng/any2jb/session"
)

type engineResult struct {
	Verified      bool              `json:"verified"`
	Error         any               `json:"error"`
	Completed     string            `json:"completed"`
	CleanupErrors []json.RawMessage `json:"cleanupErrors"`
	FS            struct {
		Verified         bool `json:"verified"`
		Restored         bool `json:"restored"`
		RetainedEndpoint struct {
			Ready   bool   `json:"ready"`
			Session string `json:"session"`
		} `json:"retainedEndpoint"`
		Cleanup struct {
			CleanedUp bool `json:"cleanedUp"`
			Deferred  bool `json:"deferred"`
			Donor     struct {
				CleanedUp bool `json:"cleanedUp"`
			} `json:"donor"`
		} `json:"cleanup"`
	} `json:"fs"`
	RPC struct{ Mode, State string } `json:"rpc"`
}

func privateJSON(root, name string, target any) error {
	dir, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer dir.Close()
	info, err := dir.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !ownedFile(info) || info.Size() > 8*1024*1024 {
		return errors.New("invalid private engine artifact")
	}
	f, err := dir.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return errors.New("engine artifact changed")
	}
	data, err := io.ReadAll(io.LimitReader(f, 8*1024*1024+1))
	if err != nil {
		return err
	}
	if len(data) > 8*1024*1024 {
		return errors.New("engine artifact too large")
	}
	return json.Unmarshal(data, target)
}

// VerifyHeld re-checks one retained session's private evidence and asks its
// live loopback endpoint for a fresh status. A stale result file alone is never
// accepted, and a failed request is never replayed.
func VerifyHeld(ctx context.Context, workspace string) (session.Outcome, error) {
	return verifyResult(ctx, workspace, true)
}

func verifyResult(ctx context.Context, workspace string, held bool) (session.Outcome, error) {
	var result engineResult
	info, err := os.Lstat(workspace)
	if err != nil {
		return session.Outcome{}, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !ownedFile(info) {
		return session.Outcome{}, errors.New("invalid owned run directory")
	}
	if err := privateJSON(workspace, "result.json", &result); err != nil {
		return session.Outcome{}, err
	}
	o := session.Outcome{Verified: result.Verified, FSVerified: result.FS.Verified, Restored: result.FS.Restored,
		CleanedUp: result.FS.Cleanup.CleanedUp, RetainedReady: result.FS.RetainedEndpoint.Ready,
		CleanupDeferred: result.FS.Cleanup.Deferred, DonorCleanedUp: result.FS.Cleanup.Donor.CleanedUp,
		RPCListening: result.RPC.Mode == "held" && result.RPC.State == "listening"}
	if result.Error != nil && result.Error != "" || len(result.CleanupErrors) != 0 ||
		!o.Verified || !o.FSVerified || !o.Restored {
		return session.Outcome{}, errors.New("engine verification incomplete")
	}
	if !held {
		if !o.CleanedUp || result.Completed == "" || result.RPC.Mode == "held" && result.RPC.State != "closed" {
			return session.Outcome{}, errors.New("final engine cleanup incomplete")
		}
		return o, nil
	}
	if result.Completed != "" || !o.RetainedReady || !o.CleanupDeferred || !o.DonorCleanedUp ||
		!o.RPCListening || result.FS.RetainedEndpoint.Session != filepath.Base(workspace) {
		return session.Outcome{}, errors.New("held engine verification incomplete")
	}
	var connection struct {
		Port      int                              `json:"port"`
		Token     string                           `json:"token"`
		Addresses struct{ Session, Target string } `json:"addresses"`
	}
	if err := privateJSON(workspace, "connection.json", &connection); err != nil {
		return session.Outcome{}, err
	}
	if connection.Port < 1 || connection.Port > 65535 || connection.Addresses.Session != filepath.Base(workspace) ||
		connection.Addresses.Target != "switch2" {
		return session.Outcome{}, errors.New("held connection identity mismatch")
	}
	request := map[string]any{"command": "status"}
	if connection.Token != "" {
		request["token"] = connection.Token
	}
	body, err := json.Marshal(request)
	if err != nil {
		return session.Outcome{}, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "http://127.0.0.1:"+strconv.Itoa(connection.Port)+"/", bytes.NewReader(body))
	if err != nil {
		return session.Outcome{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("RPC redirect refused") }}
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return session.Outcome{}, errors.New("live RPC status failed")
	}
	defer response.Body.Close()
	var status struct {
		Ready bool `json:"ready"`
		Error any  `json:"error"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&status) != nil ||
		!status.Ready || status.Error != nil {
		return session.Outcome{}, errors.New("live RPC is not ready")
	}
	return o, nil
}
