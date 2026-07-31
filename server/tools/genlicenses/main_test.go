package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectLicense(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			name: "MIT",
			text: "MIT License\n\nCopyright (c) 2020 Someone\n\nPermission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the \"Software\"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify...",
			want: "MIT",
		},
		{
			name: "Apache-2.0",
			text: "                                 Apache License\n                           Version 2.0, January 2004\n                        http://www.apache.org/licenses/",
			want: "Apache-2.0",
		},
		{
			name: "BSD-3-Clause",
			text: "Copyright (c) 2009 The Go Authors. All rights reserved.\n\nRedistribution and use in source and binary forms, with or without modification, are permitted provided that the following conditions are met:\n\n   * Redistributions of source code must retain the above copyright notice.\n   * Redistributions in binary form must reproduce the above copyright notice.\n   * Neither the name of Google Inc. nor the names of its contributors may be used to endorse products.",
			want: "BSD-3-Clause",
		},
		{
			name: "BSD-2-Clause",
			text: "Copyright (c) 2015 Someone\n\nRedistribution and use in source and binary forms, with or without modification, are permitted provided that the following conditions are met:\n\n1. Redistributions of source code must retain the above copyright notice.\n2. Redistributions in binary form must reproduce the above copyright notice in the documentation.",
			want: "BSD-2-Clause",
		},
		{
			name: "ISC",
			text: "Copyright (c) 2018 Someone\n\nPermission to use, copy, modify, and/or distribute this software for any purpose with or without fee is hereby granted, provided that the above copyright notice appears in all copies.",
			want: "ISC",
		},
		{
			name: "MPL-2.0",
			text: "Mozilla Public License Version 2.0\n==================================\n\n1. Definitions",
			want: "MPL-2.0",
		},
		{
			name: "Unlicense",
			text: "This is free and unencumbered software released into the public domain.\n\nAnyone is free to copy, modify, publish, use...",
			want: "Unlicense",
		},
		{
			// The whole point of best-effort detection: a file that carries two
			// license texts must NOT be reported as either one of them.
			name: "dual license is reported as unknown, not as the first match",
			text: "This project is covered by two different licenses: MIT and Apache.\n\nPermission is hereby granted, free of charge, to any person obtaining a copy of this software, to deal in the Software without restriction...\n\n                                 Apache License\n                           Version 2.0, January 2004",
			want: licenseUnknown,
		},
		{
			name: "unrecognised text",
			text: "All rights reserved. Contact legal@example.com for terms.",
			want: licenseUnknown,
		},
		{
			name: "empty",
			text: "   \n\t\n",
			want: licenseUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, detectLicense(tc.text))
		})
	}
}

func TestRepoURL(t *testing.T) {
	tests := []struct {
		modPath string
		want    string
	}{
		{"github.com/gofiber/fiber/v3", "https://github.com/gofiber/fiber"},
		{"github.com/stretchr/testify", "https://github.com/stretchr/testify"},
		{"gitlab.com/group/project", "https://gitlab.com/group/project"},
		{"gopkg.in/yaml.v3", "https://gopkg.in/yaml.v3"},
		// Sourcehut: the host segment is git.sr.ht, and the user segment keeps
		// its ~ prefix. Guards against the dead `case "sr.ht"` this replaced.
		{"git.sr.ht/~sircmpwn/getopt", "https://git.sr.ht/~sircmpwn/getopt"},
		// No well-known host: pkg.go.dev always resolves for a published module
		// (and keeps the /vN suffix), whereas a guessed https:// URL would 404.
		{"golang.org/x/crypto", "https://pkg.go.dev/golang.org/x/crypto"},
		{"gorm.io/gorm", "https://pkg.go.dev/gorm.io/gorm"},
		{"go.yaml.in/yaml/v3", "https://pkg.go.dev/go.yaml.in/yaml/v3"},
	}

	for _, tc := range tests {
		t.Run(tc.modPath, func(t *testing.T) {
			assert.Equal(t, tc.want, repoURL(tc.modPath))
		})
	}
}

func TestDetectLicenseInDir(t *testing.T) {
	t.Run("missing dir yields unknown", func(t *testing.T) {
		assert.Equal(t, licenseUnknown, detectLicenseInDir(""))
		assert.Equal(t, licenseUnknown, detectLicenseInDir(filepath.Join(t.TempDir(), "nope")))
	})

	t.Run("no license file yields unknown", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi"), 0o644))
		assert.Equal(t, licenseUnknown, detectLicenseInDir(dir))
	})

	t.Run("reads COPYING", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "COPYING"),
			[]byte("Mozilla Public License Version 2.0\n1. Definitions"), 0o644))
		assert.Equal(t, "MPL-2.0", detectLicenseInDir(dir))
	})

	t.Run("prefers LICENSE over lower-priority candidates", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "COPYRIGHT"),
			[]byte("Mozilla Public License Version 2.0"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "LICENSE.txt"),
			[]byte("This is free and unencumbered software released into the public domain."), 0o644))
		assert.Equal(t, "Unlicense", detectLicenseInDir(dir))
	})
}
