package nix

import (
	stdctx "context"
	"errors"
	"html/template"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goreleaser/goreleaser/v2/internal/artifact"
	"github.com/goreleaser/goreleaser/v2/internal/client"
	"github.com/goreleaser/goreleaser/v2/internal/golden"
	"github.com/goreleaser/goreleaser/v2/internal/skips"
	"github.com/goreleaser/goreleaser/v2/internal/testctx"
	"github.com/goreleaser/goreleaser/v2/internal/testlib"
	"github.com/goreleaser/goreleaser/v2/pkg/config"
	"github.com/stretchr/testify/require"
)

func TestContinueOnError(t *testing.T) {
	require.True(t, Pipe{}.ContinueOnError())
}

func TestString(t *testing.T) {
	require.NotEmpty(t, Pipe{}.String())
}

func TestSkip(t *testing.T) {
	t.Run("no-nix", func(t *testing.T) {
		require.True(t, Pipe{}.Skip(testctx.New()))
	})
	t.Run("skip flag", func(t *testing.T) {
		require.True(t, New().Skip(testctx.NewWithCfg(config.Project{
			Nix: []config.Nix{{}},
		}, testctx.Skip(skips.Nix))))
	})
	t.Run("nix-all-good", func(t *testing.T) {
		testlib.CheckPath(t, "nix-hash")
		testlib.SkipIfWindows(t, "nix doesn't work on windows")
		require.False(t, New().Skip(testctx.NewWithCfg(config.Project{
			Nix: []config.Nix{{}},
		})))
	})
}

const fakeNixHashBin = "fake-nix-hash"

func TestHasher(t *testing.T) {
	t.Run("hash", func(t *testing.T) {
		t.Run("fake-nix-hash", func(t *testing.T) {
			_, err := nixHasher{fakeNixHashBin}.Hash(t.Context(), "any")
			require.ErrorIs(t, err, exec.ErrNotFound)
		})
		t.Run("valid", func(t *testing.T) {
			testlib.CheckPath(t, "nix-hash")
			testlib.SkipIfWindows(t, "nix doesn't work on windows")
			sha, err := realHasher.Hash(t.Context(), "./testdata/file.bin")
			require.NoError(t, err)
			require.Equal(t, "1n7yy95h81rziah4ppi64kr6fphwxjiq8cl70fpfrqvr0ml1xbcl", sha)
		})
	})
	t.Run("available", func(t *testing.T) {
		t.Run("no-nix-hash", func(t *testing.T) {
			require.False(t, nixHasher{fakeNixHashBin}.Available())
		})
		t.Run("valid", func(t *testing.T) {
			testlib.CheckPath(t, "nix-hash")
			testlib.SkipIfWindows(t, "nix doesn't work on windows")
			require.True(t, realHasher.Available())
		})
	})
}

func TestRunPipe(t *testing.T) {
	for _, tt := range []struct {
		name                 string
		expectDefaultErrorIs error
		expectRunErrorIs     error
		expectPublishErrorIs error
		nix                  config.Nix
	}{
		{
			name: "minimal",
			nix: config.Nix{
				IDs: []string{"foo"},
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
		{
			name:                 "invalid license",
			expectDefaultErrorIs: errInvalidLicense,
			nix: config.Nix{
				IDs:     []string{"foo"},
				License: "mitt",
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
		{
			name: "deps",
			nix: config.Nix{
				IDs: []string{"foo"},
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
				Dependencies: []config.NixDependency{
					{Name: "fish"},
					{Name: "bash"},
					linuxDep("ttyd"),
					darwinDep("chromium"),
				},
			},
		},
		{
			name: "extra-install",
			nix: config.Nix{
				IDs: []string{"foo"},
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
				Dependencies: []config.NixDependency{
					{Name: "fish"},
					{Name: "bash"},
					linuxDep("ttyd"),
					darwinDep("chromium"),
				},
				ExtraInstall: "installManPage ./manpages/foo.1.gz",
			},
		},
		{
			name: "open-pr",
			nix: config.Nix{
				Name:        "foo",
				IDs:         []string{"foo"},
				Description: "my test",
				Homepage:    "https://goreleaser.com",
				License:     "mit",
				Path:        "pkgs/foo.nix",
				Repository: config.RepoRef{
					Owner:  "foo",
					Name:   "bar",
					Branch: "update-{{.Version}}",
					PullRequest: config.PullRequest{
						Enabled: true,
					},
				},
			},
		},
		{
			name: "wrapped-in-dir",
			nix: config.Nix{
				Name:        "wrapped-in-dir",
				IDs:         []string{"wrapped-in-dir"},
				Description: "my test",
				Homepage:    "https://goreleaser.com",
				License:     "mit",
				PostInstall: `
					echo "do something"
				`,
				Install: `
					mkdir -p $out/bin
					cp foo $out/bin/foo
				`,
				ExtraInstall: "installManPage ./manpages/foo.1.gz",
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
		{
			name: "zip",
			nix: config.Nix{
				Name:        "foozip",
				IDs:         []string{"foo-zip"},
				Description: "my test",
				Homepage:    "https://goreleaser.com",
				License:     "mit",
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
		{
			name: "zip-with-dependencies",
			nix: config.Nix{
				Name:        "foozip",
				IDs:         []string{"foo-zip"},
				Description: "my test",
				Homepage:    "https://goreleaser.com",
				License:     "mit",
				Dependencies: []config.NixDependency{
					{Name: "git"},
				},
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
		{
			name: "zip-and-tar",
			nix: config.Nix{
				Name:        "foozip",
				IDs:         []string{"zip-and-tar"},
				Description: "my test",
				Homepage:    "https://goreleaser.com",
				License:     "mit",
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
		{
			name:             "unibin",
			expectRunErrorIs: ErrMultipleArchivesSamePlatform,
			nix: config.Nix{
				Name:        "unibin",
				IDs:         []string{"unibin"},
				Description: "my test",
				Homepage:    "https://goreleaser.com",
				License:     "mit",
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
		{
			name: "no-archives",
			expectRunErrorIs: errNoArchivesFound{
				goamd64: "v2",
				ids:     []string{"nopenopenope"},
			},
			nix: config.Nix{
				Name:    "no-archives",
				IDs:     []string{"nopenopenope"},
				Goamd64: "v2",
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
		{
			name: "unibin-replaces",
			nix: config.Nix{
				Name:        "unibin-replaces",
				IDs:         []string{"unibin-replaces"},
				Description: "my test",
				Homepage:    "https://goreleaser.com",
				License:     "mit",
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
		{
			name: "partial",
			nix: config.Nix{
				Name: "partial",
				IDs:  []string{"partial"},
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
		{
			name:             "no-repo-name",
			expectRunErrorIs: errNoRepoName,
			nix: config.Nix{
				Name: "doesnotmatter",
				Repository: config.RepoRef{
					Owner: "foo",
				},
			},
		},
		{
			name:             "bad-name-tmpl",
			expectRunErrorIs: &template.Error{},
			nix: config.Nix{
				Name: "{{ .Nope }}",
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
		{
			name:             "bad-description-tmpl",
			expectRunErrorIs: &template.Error{},
			nix: config.Nix{
				Description: "{{ .Nope }}",
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
		{
			name:             "bad-homepage-tmpl",
			expectRunErrorIs: &template.Error{},
			nix: config.Nix{
				Homepage: "{{ .Nope }}",
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
		{
			name:             "bad-repo-tmpl",
			expectRunErrorIs: &template.Error{},
			nix: config.Nix{
				Name: "doesnotmatter",
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "{{ .Nope }}",
				},
			},
		},
		{
			name:             "bad-skip-upload-tmpl",
			expectRunErrorIs: &template.Error{},
			nix: config.Nix{
				Name:       "doesnotmatter",
				SkipUpload: "{{ .Nope }}",
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
		{
			name:             "bad-install-tmpl",
			expectRunErrorIs: &template.Error{},
			nix: config.Nix{
				Name:    "foo",
				Install: `{{.NoInstall}}`,
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
		{
			name:             "bad-post-install-tmpl",
			expectRunErrorIs: &template.Error{},
			nix: config.Nix{
				Name:        "foo",
				PostInstall: `{{.NoPostInstall}}`,
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
		{
			name:             "bad-path-tmpl",
			expectRunErrorIs: &template.Error{},
			nix: config.Nix{
				Name: "foo",
				Path: `{{.Foo}}/bar/foo.nix`,
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
		{
			name:             "bad-release-url-tmpl",
			expectRunErrorIs: &template.Error{},
			nix: config.Nix{
				Name:        "foo",
				URLTemplate: "{{.BadURL}}",
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
		{
			name:                 "skip-upload",
			expectPublishErrorIs: errSkipUpload,
			nix: config.Nix{
				Name:       "doesnotmatter",
				SkipUpload: "true",
				IDs:        []string{"foo"},
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
		{
			name:                 "skip-upload-auto",
			expectPublishErrorIs: errSkipUploadAuto,
			nix: config.Nix{
				Name:       "doesnotmatter",
				SkipUpload: "auto",
				IDs:        []string{"foo"},
				Repository: config.RepoRef{
					Owner: "foo",
					Name:  "bar",
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			folder := t.TempDir()
			ctx := testctx.NewWithCfg(
				config.Project{
					Dist:        folder,
					ProjectName: "foo",
					Nix:         []config.Nix{tt.nix},
				},
				testctx.WithVersion("1.2.1"),
				testctx.WithCurrentTag("v1.2.1"),
				testctx.WithSemver(1, 2, 1, "rc1"),
			)
			createFakeArtifact := func(id, goos, goarch, goamd64, goarm, format string, extra map[string]any) {
				if goarch != "arm" {
					goarm = ""
				}
				if goarch != "amd64" {
					goamd64 = ""
				}
				name := "foo_" + goos + "_" + goarch + goamd64 + goarm + "." + format
				path := filepath.Join(folder, "dist", name)
				art := artifact.Artifact{
					Name:    name,
					Path:    path,
					Goos:    goos,
					Goarch:  goarch,
					Goarm:   goarm,
					Goamd64: goamd64,
					Type:    artifact.UploadableArchive,
					Extra: map[string]any{
						artifact.ExtraID:        id,
						artifact.ExtraFormat:    format,
						artifact.ExtraBinaries:  []string{"foo"},
						artifact.ExtraWrappedIn: "",
					},
				}
				maps.Copy(art.Extra, extra)
				ctx.Artifacts.Add(&art)

				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
				f, err := os.Create(path)
				require.NoError(t, err)
				require.NoError(t, f.Close())
			}

			createFakeArtifact("unibin-replaces", "darwin", "all", "", "", "tar.gz", map[string]any{artifact.ExtraReplaces: true})
			createFakeArtifact("unibin", "darwin", "all", "", "", "tgz", nil)
			for _, goos := range []string{"linux", "darwin", "windows"} {
				for _, goarch := range []string{"amd64", "arm64", "386", "arm"} {
					if goos+goarch == "darwin386" {
						continue
					}
					if goarch == "amd64" {
						createFakeArtifact("partial", goos, goarch, "v1", "", "tar.xz", nil)
						createFakeArtifact("foo", goos, goarch, "v1", "", "txz", nil)
						createFakeArtifact("unibin", goos, goarch, "v1", "", "tar.zst", nil)
						if goos != "darwin" {
							createFakeArtifact("unibin-replaces", goos, goarch, "v1", "", "tzst", nil)
						}
						createFakeArtifact("wrapped-in-dir", goos, goarch, "v1", "", "tar", map[string]any{artifact.ExtraWrappedIn: "./foo_" + goarch})
						createFakeArtifact("foo-zip", goos, goarch, "v1", "", "zip", nil)
						continue
					}
					if goarch == "arm" {
						if goos != "linux" {
							continue
						}
						createFakeArtifact("foo", goos, goarch, "", "6", "tar.gz", nil)
						createFakeArtifact("foo", goos, goarch, "", "7", "tar.gz", nil)
						createFakeArtifact("foo-zip", goos, goarch, "", "", "zip", nil)
						createFakeArtifact("unibin-replaces", goos, goarch, "", "", "tar.gz", nil)
						continue
					}
					createFakeArtifact("foo", goos, goarch, "", "", "tar.gz", nil)
					createFakeArtifact("unibin", goos, goarch, "", "", "tar.gz", nil)
					if goos != "darwin" {
						createFakeArtifact("unibin-replaces", goos, goarch, "", "", "tar.gz", nil)
					}
					createFakeArtifact("wrapped-in-dir", goos, goarch, "", "", "tar.gz", map[string]any{artifact.ExtraWrappedIn: "./foo_" + goarch})
					createFakeArtifact("foo-zip", goos, goarch, "v1", "", "zip", nil)
					if goos == "darwin" {
						createFakeArtifact("zip-and-tar", goos, goarch, "v1", "", "zip", nil)
					} else {
						createFakeArtifact("zip-and-tar", goos, goarch, "v1", "", "tar.gz", nil)
					}
				}
			}

			client := client.NewMock()
			bpipe := Pipe{
				alwaysZeroHasher{},
			}
			ppipe := Pipe{
				fakeHasher{
					"foo_linux_amd64v1.txz":     "sha1",
					"foo_linux_amd64v1.tzst":    "sha1",
					"foo_linux_amd64v1.tar.xz":  "sha1",
					"foo_linux_amd64v1.tar":     "sha1",
					"foo_linux_amd64v1.tar.gz":  "sha1",
					"foo_linux_arm64.tar.gz":    "sha2",
					"foo_darwin_amd64v1.txz":    "sha3",
					"foo_darwin_amd64v1.tar":    "sha3",
					"foo_darwin_amd64v1.tar.xz": "sha3",
					"foo_darwin_arm64.tar.gz":   "sha4",
					"foo_darwin_all.tgz":        "sha5",
					"foo_darwin_all.tar.gz":     "sha5",
					"foo_linux_arm6.tar.gz":     "sha6",
					"foo_linux_arm7.tar.gz":     "sha7",
					"foo_linux_amd64v1.zip":     "sha8",
					"foo_linux_arm64.zip":       "sha9",
					"foo_darwin_amd64v1.zip":    "sha10",
					"foo_darwin_arm64.zip":      "sha11",
					"foo_darwin_all.zip":        "sha12",
					"foo_linux_arm6.zip":        "sha13",
					"foo_linux_arm7.zip":        "sha14",
					"foo_linux_386.zip":         "sha15",
					"foo_linux_386.tar.gz":      "sha16",
				},
			}

			// default
			if tt.expectDefaultErrorIs != nil {
				err := bpipe.Default(ctx)
				require.ErrorAs(t, err, &tt.expectDefaultErrorIs)
				return
			}
			require.NoError(t, bpipe.Default(ctx))

			// run
			if tt.expectRunErrorIs != nil {
				err := bpipe.runAll(ctx, client)
				require.ErrorAs(t, err, &tt.expectRunErrorIs)
				return
			}
			require.NoError(t, bpipe.runAll(ctx, client))
			bts, err := os.ReadFile(ctx.Artifacts.Filter(artifact.ByType(artifact.Nixpkg)).Paths()[0])
			require.NoError(t, err)
			golden.RequireEqualExt(t, bts, "_build.nix")

			// publish
			if tt.expectPublishErrorIs != nil {
				err := ppipe.publishAll(ctx, client)
				require.ErrorAs(t, err, &tt.expectPublishErrorIs)
				return
			}
			require.NoError(t, ppipe.publishAll(ctx, client))
			require.True(t, client.CreatedFile)
			golden.RequireEqualExt(t, []byte(client.Content), "_publish.nix")
			require.NotContains(t, client.Content, strings.Repeat("0", 52))

			if tt.nix.Repository.PullRequest.Enabled {
				require.True(t, client.OpenedPullRequest)
				require.True(t, client.SyncedFork)
			}
			if tt.nix.Path != "" {
				require.Equal(t, tt.nix.Path, client.Path)
			} else {
				if tt.nix.Name == "" {
					tt.nix.Name = "foo"
				}
				require.Equal(t, "pkgs/"+tt.nix.Name+"/default.nix", client.Path)
			}
		})
	}
}

func TestRunSkipNoNix(t *testing.T) {
	ctx := testctx.WrapWithCfg(t.Context(), config.Project{
		Nix: []config.Nix{
			{},
		},
	})
	p := Pipe{}
	p.hasher = unavailableHasher{}
	require.NoError(t, p.Default(ctx))
	testlib.AssertSkipped(t, p.Run(ctx))
}

func TestErrNoArchivesFound(t *testing.T) {
	require.EqualError(t, errNoArchivesFound{
		goamd64: "v1",
		ids:     []string{"foo", "bar"},
	}, "no archives found matching goos=[darwin linux] goarch=[amd64 arm arm64 386] goarm=[6 7] goamd64=v1 ids=[foo bar]")
}

func TestDependencies(t *testing.T) {
	require.Equal(t, []string{"nix-hash"}, Pipe{}.Dependencies(nil))
}

func TestBinInstallFormats(t *testing.T) {
	t.Run("no-deps", func(t *testing.T) {
		golden.RequireEqual(t, []byte(strings.Join(
			binInstallFormats(config.Nix{}),
			"\n",
		)))
	})
	t.Run("deps", func(t *testing.T) {
		golden.RequireEqual(t, []byte(strings.Join(
			binInstallFormats(config.Nix{
				Dependencies: []config.NixDependency{
					{Name: "fish"},
					{Name: "bash"},
					{Name: "zsh"},
				},
			}),
			"\n",
		)))
	})
	t.Run("linux-only-deps", func(t *testing.T) {
		golden.RequireEqual(t, []byte(strings.Join(
			binInstallFormats(config.Nix{
				Dependencies: []config.NixDependency{
					linuxDep("foo"),
					linuxDep("bar"),
				},
			}),
			"\n",
		)))
	})
	t.Run("darwin-only-deps", func(t *testing.T) {
		golden.RequireEqual(t, []byte(strings.Join(
			binInstallFormats(config.Nix{
				Dependencies: []config.NixDependency{
					darwinDep("foo"),
					darwinDep("bar"),
				},
			}),
			"\n",
		)))
	})
	t.Run("mixed-deps", func(t *testing.T) {
		golden.RequireEqual(t, []byte(strings.Join(
			binInstallFormats(config.Nix{
				Dependencies: []config.NixDependency{
					{Name: "fish"},
					linuxDep("foo"),
					darwinDep("bar"),
				},
			}),
			"\n",
		)))
	})
}

func darwinDep(s string) config.NixDependency {
	return config.NixDependency{
		Name: s,
		OS:   "darwin",
	}
}

func linuxDep(s string) config.NixDependency {
	return config.NixDependency{
		Name: s,
		OS:   "linux",
	}
}

type unavailableHasher struct{}

func (m unavailableHasher) Hash(stdctx.Context, string) (string, error) {
	return "", errors.New("unavailable hasher")
}
func (m unavailableHasher) Available() bool { return false }

type fakeHasher map[string]string

func (m fakeHasher) Hash(_ stdctx.Context, path string) (string, error) {
	return m[filepath.Base(path)], nil
}
func (m fakeHasher) Available() bool { return true }

const zeroHash = "0000000000000000000000000000000000000000000000000000"

type alwaysZeroHasher struct{}

func (alwaysZeroHasher) Hash(stdctx.Context, string) (string, error) { return zeroHash, nil }
func (alwaysZeroHasher) Available() bool                             { return true }
