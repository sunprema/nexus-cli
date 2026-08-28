package cli

import (
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
)

// gitAuthorFromRepo resolves the author identity to use for a commit Nexus
// creates itself (e.g. the orphan 'explainer' branch's init commit).
//
// ConfigScoped merges local + global (local wins), matching git's own
// resolution, via the ConfigLoader plugin registered in configloader.go (a
// symlink-following Auto loader so global config behind a symlinked
// ~/.config, e.g. managed by chezmoi/Stow/yadm, is still read).
func gitAuthorFromRepo(repo *git.Repository) (name, email string) {
	if cfg, err := repo.ConfigScoped(config.GlobalScope); err == nil {
		name = cfg.User.Name
		email = cfg.User.Email
	}

	if name == "" || email == "" {
		//nolint:staticcheck // the v6 is not yet released, revisit once it is.
		globalCfg, err := config.LoadConfig(config.GlobalScope)
		if err == nil {
			if name == "" {
				name = globalCfg.User.Name
			}
			if email == "" {
				email = globalCfg.User.Email
			}
		}
	}

	if name == "" {
		name = "Unknown"
	}
	if email == "" {
		email = "unknown@local"
	}
	return name, email
}
