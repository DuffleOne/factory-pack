package pack

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// DefaultServer is the factory planner.
const DefaultServer = "https://factory.duffle.one"

// Client sends exports to a factory planner.
type Client struct {
	// Server is the planner's address, DefaultServer when empty.
	Server string
	// HTTP is the client to send with, a plain one when nil.
	HTTP *http.Client
	// Agent is what the tool calls itself in each request.
	Agent string
	// Poll is how often to ask whether a pack is ready, every two seconds
	// when zero.
	Poll time.Duration
}

// Pack is what the planner says of a pack.
type Pack struct {
	// Code is what to type in on the planner to plan with it.
	Code string `json:"code"`
	// State is "uploading", "building", "ready" or "failed".
	State   string `json:"state"`
	Message string `json:"message,omitempty"`
	Name    string `json:"name,omitempty"`
	// Expires is when it goes unless it's used before then, RFC 3339.
	Expires string `json:"expires,omitempty"`
	// Token lets the sender add pictures and start the build. Only the
	// first answer for an upload has it.
	Token string `json:"token,omitempty"`
	// Pictures are what the planner wants sent, by the paths prototypes
	// use. Only the first answer for an upload has them.
	Pictures []string `json:"pictures,omitempty"`
	// Batch is the most to send in one go.
	Batch int64 `json:"batch,omitempty"`
}

// Sent is what sending an export came to.
type Sent struct {
	Pack
	// Pictures is what was sent, and Missing what was asked for but
	// couldn't be read or wasn't sent, with why.
	Pictures []File            `json:"pictures"`
	Missing  map[string]string `json:"missing,omitempty"`
}

// Send sends an export, then the pictures the planner asks for, read by
// pics, then waits while the planner builds the pack, and gives back its
// code. keep, when set, is called with each picture sent, so a copy can be
// kept of everything that went.
func (c *Client) Send(ctx context.Context, export string, pics *Pictures, progress func(Step), keep func(name string, b []byte) error) (*Sent, error) {
	if progress == nil {
		progress = func(Step) {}
	}

	f, err := os.Open(export)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}

	size := st.Size()
	going := func(n int64) {
		progress(Step{Key: "export", Stage: "sending what the game wrote", Done: n, Of: size, Bytes: true})
	}

	going(0)

	var got Pack
	if err := c.do(ctx, http.MethodPost, "/api/packs", "", "application/gzip", counted(f, going), size, &got); err != nil {
		return nil, err
	}

	sent := &Sent{Pack: got, Missing: map[string]string{}}

	if got.State == "uploading" {
		if err := c.pictures(ctx, sent, pics, progress, keep); err != nil {
			return nil, err
		}

		progress(Step{Key: "build", Stage: "the planner is building your pack"})

		body, err := json.Marshal(map[string]any{"missing": sent.Missing})
		if err != nil {
			return nil, err
		}

		if err := c.do(ctx, http.MethodPost, "/api/packs/"+got.Code+"/build", got.Token, "application/json", bytes.NewReader(body), int64(len(body)), &got); err != nil {
			return nil, err
		}
	}

	ready, err := c.wait(ctx, got.Code, progress)
	if err != nil {
		return nil, err
	}

	sent.Pack = *ready

	return sent, nil
}

// pictures sends what the planner asked for, a batch at a time. One that
// can't be read is noted and left out: the pack comes without that picture.
// How far it's got counts pictures passed over and pictures sent, a batch's
// worth moving along as its bytes go.
func (c *Client) pictures(ctx context.Context, sent *Sent, pics *Pictures, progress func(Step), keep func(string, []byte) error) error {
	batch := sent.Batch
	if batch <= 0 {
		batch = 32 << 20
	}

	total := int64(len(sent.Pack.Pictures))

	var (
		buf  bytes.Buffer
		tw   = tar.NewWriter(&buf)
		in   int64
		done int64
	)

	tell := func(n int64) {
		progress(Step{Key: "pictures", Stage: "sending pictures", Done: n, Of: total})
	}

	tell(0)

	flush := func() error {
		if in == 0 {
			return nil
		}

		if err := tw.Close(); err != nil {
			return err
		}

		body, size := buf.Bytes(), int64(buf.Len())
		going := func(n int64) { tell(done + in*n/max(size, 1)) }

		if err := c.retry(ctx, func() error {
			return c.do(ctx, http.MethodPost, "/api/packs/"+sent.Code+"/pictures", sent.Token, "application/x-tar", counted(bytes.NewReader(body), going), size, nil)
		}); err != nil {
			return err
		}

		done += in
		buf.Reset()
		tw, in = tar.NewWriter(&buf), 0

		tell(done)

		return nil
	}

	for _, name := range sent.Pack.Pictures {
		b, err := pics.Read(name)
		if err != nil {
			sent.Missing[name] = err.Error()
			done++

			continue
		}

		if int64(buf.Len()+len(b)) > batch {
			if err := flush(); err != nil {
				return err
			}
		}

		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(b)), Typeflag: tar.TypeReg, Format: tar.FormatPAX}); err != nil {
			return err
		}

		if _, err := tw.Write(b); err != nil {
			return err
		}

		in++

		sent.Pictures = append(sent.Pictures, File{Name: name, Size: int64(len(b))})

		if keep != nil {
			if err := keep(name, b); err != nil {
				return err
			}
		}
	}

	return flush()
}

// wait asks after a pack until it's built, or the build fails.
func (c *Client) wait(ctx context.Context, code string, progress func(Step)) (*Pack, error) {
	every := c.Poll
	if every <= 0 {
		every = 2 * time.Second
	}

	for {
		var p Pack
		if err := c.do(ctx, http.MethodGet, "/api/packs/"+code, "", "", nil, 0, &p); err != nil {
			return nil, err
		}

		switch p.State {
		case "ready":
			return &p, nil
		case "failed":
			return nil, fmt.Errorf("the planner couldn't build the pack: %s", p.Message)
		}

		progress(Step{Key: "build", Stage: "the planner is building your pack"})

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(every):
		}
	}
}

// counter tells how much of a body has gone, every so often and at the
// end.
type counter struct {
	r    io.Reader
	n    int64
	last time.Time
	tell func(n int64)
}

func counted(r io.Reader, tell func(n int64)) io.Reader { return &counter{r: r, tell: tell} }

func (c *counter) Read(p []byte) (int, error) {
	k, err := c.r.Read(p)
	c.n += int64(k)

	if now := time.Now(); err != nil || now.Sub(c.last) >= 150*time.Millisecond {
		c.last = now
		c.tell(c.n)
	}

	return k, err
}

// retry tries a send again, a few times and further apart each time, when
// it fails for want of a connection or a server too busy to answer.
func (c *Client) retry(ctx context.Context, send func() error) error {
	wait := 2 * time.Second

	for try := 1; ; try++ {
		err := send()

		var bad *Refusal
		if err == nil || try == 4 || ctx.Err() != nil || (errors.As(err, &bad) && bad.Status < 500) {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}

		wait *= 2
	}
}

// Refusal is the planner saying no.
type Refusal struct {
	Status  int
	Message string
}

func (r *Refusal) Error() string { return r.Message }

// do sends a request with a body of size bytes, if it has one, and reads
// the answer into out.
func (c *Client) do(ctx context.Context, method, route, token, kind string, body io.Reader, size int64, out any) error {
	base := strings.TrimRight(c.Server, "/")
	if base == "" {
		base = DefaultServer
	}

	u, err := url.Parse(base + route)
	if err != nil {
		return fmt.Errorf("the planner's address doesn't read as one: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return err
	}
	// Said up front, so the planner can turn away what's too big before
	// it all arrives.
	if body != nil {
		req.ContentLength = size
	}

	if kind != "" {
		req.Header.Set("Content-Type", kind)
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	if c.Agent != "" {
		req.Header.Set("User-Agent", c.Agent)
	}

	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Minute}
	}

	res, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("couldn't reach the planner at %s: %w", base, err)
	}
	defer func() { _ = res.Body.Close() }()

	b, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return err
	}

	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}

		msg := strings.TrimSpace(string(b))
		if json.Unmarshal(b, &e) == nil && e.Error != "" {
			msg = e.Error
		}

		if len(msg) > 300 {
			msg = msg[:300]
		}

		return &Refusal{Status: res.StatusCode, Message: fmt.Sprintf("the planner said no (%d): %s", res.StatusCode, msg)}
	}

	if out == nil {
		return nil
	}

	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("the planner's answer didn't make sense: %w", err)
	}

	return nil
}
