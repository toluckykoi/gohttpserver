package server

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/go-yaml/yaml"
)

// TestAccessConfMergeMatchesYAMLUnmarshal pins the behaviour the cached
// parser has to preserve: the original code unmarshalled each .ghs.yml
// on top of the already-merged parent config, so a child file overrides
// only the keys it declares. rawAccessConf + mergeInto must reach the
// exact same result as that in-place yaml.Unmarshal.
func TestAccessConfMergeMatchesYAMLUnmarshal(t *testing.T) {
	base := AccessConf{
		Upload:       true,
		Delete:       true,
		Edit:         true,
		Users:        []UserControl{{Email: "base@example.com", Upload: true, Delete: true, Edit: true, Token: "base"}},
		AccessTables: []AccessTable{{Regex: "^secret/", Allow: false}},
	}

	cases := map[string]string{
		"empty":                  "",
		"upload only":            "upload: false\n",
		"delete only":            "delete: false\n",
		"edit only":              "edit: false\n",
		"all bools":              "upload: false\ndelete: false\nedit: false\n",
		"users replaced":         "users:\n  - email: child@example.com\n    upload: true\n    delete: false\n    edit: true\n    token: child\n",
		"users emptied":          "users: []\n",
		"access tables replaced": "accessTables:\n  - regex: ^public/\n    allow: true\n",
		"bools and tables":       "upload: false\naccessTables: []\n",
	}

	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			// Reference: the original in-place yaml.Unmarshal merge.
			want := base
			if err := yaml.Unmarshal([]byte(doc), &want); err != nil {
				t.Fatalf("reference unmarshal failed: %v", err)
			}

			// New: cached parse + explicit merge.
			raw, err := parseAccessConfFile([]byte(doc))
			if err != nil {
				t.Fatalf("cached parse failed: %v", err)
			}
			got := base
			raw.mergeInto(&got)

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("merge mismatch\n got: %#v\nwant: %#v", got, want)
			}
		})
	}
}

// TestReadAccessConfInheritsAndOverrides checks the directory walk end
// to end: a child .ghs.yml overrides one key while inheriting the rest
// from the root config.
func TestReadAccessConfInheritsAndOverrides(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "sub")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	rootConf := "upload: true\ndelete: true\nedit: false\nusers:\n  - email: root@example.com\n    upload: true\n    delete: true\n    edit: false\n    token: roottoken\n"
	if err := os.WriteFile(filepath.Join(root, YAMLCONF), []byte(rootConf), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, YAMLCONF), []byte("upload: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewHTTPStaticServer(root, true)

	got := s.readAccessConf(child)
	if got.Upload {
		t.Fatalf("child upload override not applied: %#v", got)
	}
	if !got.Delete {
		t.Fatalf("delete should be inherited from root: %#v", got)
	}
	if got.Edit {
		t.Fatalf("edit should be inherited from root (false): %#v", got)
	}
	if len(got.Users) != 1 || got.Users[0].Email != "root@example.com" {
		t.Fatalf("users should be inherited from root: %#v", got.Users)
	}
}

// TestReadAccessConfCacheInvalidation verifies that a rewrite of
// .ghs.yml is observed once the handler invalidates the cache, which is
// what upload/delete/edit do after a mutation.
func TestReadAccessConfCacheInvalidation(t *testing.T) {
	root := t.TempDir()
	confPath := filepath.Join(root, YAMLCONF)
	if err := os.WriteFile(confPath, []byte("upload: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := NewHTTPStaticServer(root, true)
	if s.readAccessConf(root).Upload {
		t.Fatal("expected upload=false from initial config")
	}

	if err := os.WriteFile(confPath, []byte("upload: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.invalidateAccessConf()

	if !s.readAccessConf(root).Upload {
		t.Fatal("expected upload=true after invalidation and rewrite")
	}
}
