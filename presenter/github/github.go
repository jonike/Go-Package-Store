// Package github provides a GitHub API-powered presenter. It supports repositories that are on github.com.
package github

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/google/go-github/github"
	"github.com/shurcooL/Go-Package-Store/pkg"
	"github.com/shurcooL/Go-Package-Store/presenter"
)

// SetClient sets a custom HTTP client for accessing the GitHub API by this presenter.
// By default, http.DefaultClient is used.
//
// It should not be called while the presenter is in use.
func SetClient(client *http.Client) {
	gh = github.NewClient(client)
}

// gh is the GitHub API client used by this presenter.
var gh = github.NewClient(nil)

func init() {
	presenter.RegisterProvider(func(repo *pkg.Repo) presenter.Presenter {
		switch {
		case strings.HasPrefix(repo.Root, "github.com/"):
			elems := strings.Split(repo.Root, "/")
			if len(elems) != 3 {
				return nil
			}
			return newGitHubPresenter(repo, elems[1], elems[2])
		// azul3d.org package (an instance of semver-based domain, see https://azul3d.org/semver).
		// Once there are other semver based Go packages, consider adding more generalized support.
		case strings.HasPrefix(repo.Root, "azul3d.org/"):
			gitHubOwner, gitHubRepo, err := azul3dOrgImportPathToGitHub(repo.Root)
			if err != nil {
				return nil
			}
			return newGitHubPresenter(repo, gitHubOwner, gitHubRepo)
		// gopkg.in package.
		case strings.HasPrefix(repo.Root, "gopkg.in/"):
			gitHubOwner, gitHubRepo, err := gopkgInImportPathToGitHub(repo.Root)
			if err != nil {
				return nil
			}
			return newGitHubPresenter(repo, gitHubOwner, gitHubRepo)
		// Underlying GitHub remote.
		case strings.HasPrefix(repo.RemoteURL, "https://github.com/"):
			elems := strings.Split(strings.TrimSuffix(repo.RemoteURL[len("https://"):], ".git"), "/")
			if len(elems) != 3 {
				return nil
			}
			return newGitHubPresenter(repo, elems[1], elems[2])
		// Go repo remote has a GitHub mirror repo.
		case strings.HasPrefix(repo.RemoteURL, "https://go.googlesource.com/"):
			repoName := repo.RemoteURL[len("https://go.googlesource.com/"):]
			return newGitHubPresenter(repo, "golang", repoName)
		default:
			return nil
		}
	})
}

type gitHubPresenter struct {
	repo    *pkg.Repo
	ghOwner string
	ghRepo  string

	cc    *github.CommitsComparison
	image template.URL
	err   error
}

func newGitHubPresenter(repo *pkg.Repo, ghOwner, ghRepo string) *gitHubPresenter {
	p := &gitHubPresenter{
		repo:    repo,
		ghOwner: ghOwner,
		ghRepo:  ghRepo,

		image: "https://github.com/images/gravatars/gravatar-user-420.png", // Default fallback.
	}

	// This might take a while.
	if cc, _, err := gh.Repositories.CompareCommits(ghOwner, ghRepo, repo.Local.Revision, repo.Remote.Revision); err == nil {
		p.cc = cc
	} else if rateLimitErr, ok := err.(*github.RateLimitError); ok {
		p.setFirstError(rateLimitError{rateLimitErr})
	} else {
		p.setFirstError(fmt.Errorf("gh.Repositories.CompareCommits: %v", err))
	}

	// Use the repo owner avatar image.
	if user, _, err := gh.Users.Get(ghOwner); err == nil && user.AvatarURL != nil {
		p.image = template.URL(*user.AvatarURL)
	} else if rateLimitErr, ok := err.(*github.RateLimitError); ok {
		p.setFirstError(rateLimitError{rateLimitErr})
	} else {
		p.setFirstError(fmt.Errorf("gh.Users.Get: %v", err))
	}

	return p
}

func (p gitHubPresenter) Home() *template.URL {
	switch {
	case strings.HasPrefix(p.repo.Root, "github.com/"):
		url := template.URL("https://github.com/" + p.ghOwner + "/" + p.ghRepo)
		return &url
	default:
		url := template.URL("http://" + p.repo.Root)
		return &url
	}
}

func (p gitHubPresenter) Image() template.URL {
	return p.image
}

func (p gitHubPresenter) Changes() <-chan presenter.Change {
	if p.cc == nil {
		return nil
	}
	out := make(chan presenter.Change)
	go func() {
		for index := range p.cc.Commits {
			change := presenter.Change{
				Message: firstParagraph(*p.cc.Commits[len(p.cc.Commits)-1-index].Commit.Message),
				URL:     template.URL(*p.cc.Commits[len(p.cc.Commits)-1-index].HTMLURL),
			}
			if commentCount := p.cc.Commits[len(p.cc.Commits)-1-index].Commit.CommentCount; commentCount != nil && *commentCount > 0 {
				change.Comments.Count = *commentCount
				change.Comments.URL = template.URL(*p.cc.Commits[len(p.cc.Commits)-1-index].HTMLURL + "#comments")
			}
			out <- change
		}
		close(out)
	}()
	return out
}

func (p gitHubPresenter) Error() error { return p.err }

// setFirstError sets error if it's the first one. It does nothing otherwise.
func (p *gitHubPresenter) setFirstError(err error) {
	if p.err != nil {
		return
	}
	p.err = err
}

// rateLimitError is an error presentation wrapper for consistent display of *github.RateLimitError.
type rateLimitError struct {
	err *github.RateLimitError
}

func (r rateLimitError) Error() string {
	return fmt.Sprintf("GitHub API rate limit exceeded; it will be reset in %v", humanize.Time(r.err.Rate.Reset.Time))
}

// firstParagraph returns the first paragraph of a string.
func firstParagraph(s string) string {
	index := strings.Index(s, "\n\n")
	if index == -1 {
		return s
	}
	return s[:index]
}
