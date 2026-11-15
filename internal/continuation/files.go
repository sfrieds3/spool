package continuation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sfrieds3/spool/internal/config"
	"github.com/sfrieds3/spool/internal/handoff"
	"github.com/sfrieds3/spool/internal/history"
	"github.com/sfrieds3/spool/internal/safefile"
)

// SourceIdentity binds a full validated source to an exact regular file snapshot.
type SourceIdentity struct {
	session history.Session
	info    os.FileInfo
	hash    [32]byte
}

func (s *Service) sourceIdentity(ctx context.Context, sess *history.Session) (*SourceIdentity, error) {
	var src *config.Source
	for i := range s.Sources {
		if s.Sources[i].ID == sess.SourceID && s.Sources[i].Enabled {
			src = &s.Sources[i]
			break
		}
	}
	if src == nil {
		return nil, errors.New("source is not configured")
	}
	root, err := os.OpenRoot(src.Root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, info, err := safefile.OpenRegular(root, sess.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, &contextReader{ctx: ctx, r: io.NewSectionReader(f, 0, info.Size())}); err != nil {
		return nil, err
	}
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return nil, errors.New("source changed while capturing identity")
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return &SourceIdentity{session: *sess, info: info, hash: sum}, nil
}
func (s *Service) revalidate(ctx context.Context, old *SourceIdentity) error {
	if old == nil {
		return errors.New("source approval is missing")
	}
	sess, herr := s.History.Describe(ctx, old.session.Ref)
	if herr != nil {
		return herr
	}
	now, err := s.sourceIdentity(ctx, sess)
	if err != nil {
		return err
	}
	if !os.SameFile(old.info, now.info) || old.hash != now.hash || old.session.OriginalPath != sess.OriginalPath || old.session.Kind != sess.Kind || str(old.session.Cwd) != str(sess.Cwd) {
		return errors.New("source changed; review and confirm again")
	}
	return nil
}
func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func CheckWorkspace(p string) error {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p {
		return errors.New("workspace must be an absolute clean existing directory")
	}
	info, err := os.Stat(p)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("workspace is not a directory")
	}
	return nil
}

// PrivateDir creates owner-only temporary storage only after checking its
// prospective location against all history roots. Rejected locations create nothing.
func PrivateDir(sources []config.Source) (string, error) {
	for attempt := 0; attempt < 8; attempt++ {
		var suffix [16]byte
		if _, err := rand.Read(suffix[:]); err != nil {
			return "", err
		}
		base, err := filepath.Abs(os.TempDir())
		if err != nil {
			return "", err
		}
		dir := filepath.Join(base, fmt.Sprintf("spool-continuation-%x", suffix))
		if err := handoff.CheckExclusion(dir, sources); err != nil {
			return "", err
		}
		if err := os.Mkdir(dir, 0700); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return "", err
		}
		if err := handoff.CheckExclusion(dir, sources); err != nil {
			os.Remove(dir)
			return "", err
		}
		return dir, nil
	}
	return "", errors.New("could not allocate private temporary directory")
}

// contextReader limits each cancellation check to one bounded filesystem read.
type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) > 64<<10 {
		p = p[:64<<10]
	}
	return r.r.Read(p)
}
