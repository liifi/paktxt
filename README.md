# paktxt

A command-line tool that consolidates multiple text files into a single structured `.paktxt` file and accurately restores them with original directory structure and executable flags.

## Quick Examples

```bash
# 1. Pack current directory to clipboard (skips .git, binaries, etc.)
paktxt pack --clipboard

# 2. Pack directory to file, then unpack it
paktxt pack --output-file my_project.paktxt
paktxt unpack --paktxt-file my_project.paktxt

# 3. Pack with exclusions
paktxt pack -b --exclude '*.log,temp/*'

# 4. Smart filtering - only text files are included
# Git-aware: includes tracked, staged, and untracked files (respects .gitignore)
# Non-git: recursively scans directory (skips .git/, node_modules/, binaries, temp files)
paktxt pack --output-file archive.paktxt

# 5. Unpack to specific directory
paktxt unpack --paktxt-file archive.paktxt --working-dir /new/location
```

## How It Works

Instead of managing many separate files, you consolidate them into a single portable archive:

```bash
# Before: Multiple scattered files 😟
my_project/
├── src/main.go
├── README.md
├── config/app.yaml
└── scripts/build.sh

# After: Single consolidated file 😎
paktxt pack --output-file my_project.paktxt
# Creates one structured file containing all content + metadata
```

`paktxt` preserves directory structure, executable flags, and provides intelligent filtering while creating human-readable, machine-parsable archives.

## Why paktxt?

Perfect for scenarios where you need portable, consolidated project data:

- **📤 Sharing project snapshots** - Send entire codebases as single files
- **💾 Creating single-file backups** - Archive projects without compression complexity  
- **🤖 Providing context to LLMs** - Give AI models complete project context without file fragmentation
- **🌐 Cross-platform compatibility** - Preserve file structures and executable flags across different systems
- **📝 Human-readable archives** - Unlike binary formats, `.paktxt` files remain inspectable and editable

## Installation

### Pre-built Binaries

Download the latest release from [GitHub Releases](https://github.com/liifi/paktxt/releases):

```bash
# Download for your platform
curl -L https://github.com/liifi/paktxt/releases/latest/download/paktxt_linux_amd64.tar.gz | tar xz
sudo mv paktxt /usr/local/bin/
```

### Package Managers

[![Packaging status](https://repology.org/badge/vertical-allrepos/paktxt.svg)](https://repology.org/project/paktxt/versions)

```bash
# Windows (Scoop)
scoop install paktxt

# Cross-platform (Pixi - modern package manager)
pixi add paktxt
```

Check [Repology](https://repology.org/project/paktxt/versions) for the latest packaging status across distributions.

### From Source

```bash
go install github.com/liifi/paktxt@latest
```

## Commands

### pack - Consolidate Files

The `pack` command scans a directory for text-based files, intelligently ignoring binaries, temp files, and common directories like `.git` and `node_modules`. It prioritizes `README.md` files to appear first.

**Git-Aware Behavior**: When run inside a git repository, `pack` uses git-aware file scanning that includes:
- All tracked files (committed to git)
- Staged files (added to the index with `git add`)
- Untracked files (not ignored by `.gitignore`)

This ensures that only files relevant to your project are included while respecting your `.gitignore` patterns. In non-git directories, it falls back to recursive directory scanning.

#### Basic Usage

```bash
# Pack current directory to clipboard
paktxt pack --clipboard
# or
paktxt pack -b

# Pack to file
paktxt pack --output-file my_project.paktxt
# or  
paktxt pack -o my_project.paktxt

# Pack specific directory
paktxt pack --working-dir /path/to/code -o archive.paktxt
# or
paktxt pack -w /path/to/code -o archive.paktxt
```

#### Filtering Options

```bash
# Exclude files/patterns
paktxt pack -b --exclude '*.log,temp/*'
# or
paktxt pack -b -e '*.log,temp/*'

# Include only specific patterns
paktxt pack -b --filter '*.go,*.js,*.css'
# or
paktxt pack -b -f '*.go,*.js,*.css'
```

### unpack - Restore Files

The `unpack` command reads `.paktxt` content and recreates the original files and directories with proper executable flags.

#### Basic Usage

```bash
# Unpack from clipboard
paktxt unpack --clipboard
# or
paktxt unpack -b

# Unpack from file
paktxt unpack --paktxt-file archive.paktxt
# or
paktxt unpack -i archive.paktxt

# Unpack to specific directory
paktxt unpack -i archive.paktxt --working-dir /target/location
# or
paktxt unpack -i archive.paktxt -w /target/location
```

#### Selective Restore

```bash
# Exclude files during unpack
paktxt unpack -b --exclude 'config.json,secrets.txt'
# or
paktxt unpack -b -e 'config.json,secrets.txt'

# Restore only specific patterns
paktxt unpack -b --filter '*.html,*.css'
# or
paktxt unpack -b -f '*.html,*.css'
```

## File Format

Each file's content, along with its relative path and executable status, is embedded within unique delimited blocks:

```
---PAKTXT_FILE_START-19f8e7d6-c5b4-a321-b0e9-f8a7d6c5b4a3---
filename: my_module/utility.go
executable: false
content:
package my_module

func UtilityFunction() {
    // Some code here
}
---PAKTXT_FILE_END-19f8e7d6-c5b4-a321-b0e9-f8a7d6c5b4a3---
```

The GUID-based delimiters ensure reliable parsing even with complex file contents.

---

For more options and advanced usage, run `paktxt --help`.