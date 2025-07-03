package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/atotto/clipboard"
)

// Version of the paktxt application. This will be set by Goreleaser via linker flags.
var version = "dev"

// Delimiter and identifier used in the Markdown file
const (
	startBlockDelimiter  = "---PAKTXT" + "_FILE_START-19f8e7d6-c5b4-a321-b0e9-f8a7d6c5b4a3---"
	endBlockDelimiter    = "---PAKTXT" + "_FILE_END-19f8e7d6-c5b4-a321-b0e9-f8a7d6c5b4a3---"
	filenameLabel        = "filename: "
	executableLabel      = "executable: "
	trailingNewlineLabel = "trailing_newline: "
	contentLabel         = "content:\n"
	mdExtension          = ".md"
	paktxtExtension      = ".paktxt"
)

const paktxtHeader = `PAKTXT
This document contains a collection of text-based files from a directory,
concatenated into a single .paktxt file by the 'paktxt' Go program.

Each file's content is embedded within distinct blocks, defined by unique start and end delimiters.
The original file path is specified by a 'filename:' label,
its executable status by an 'executable:' label, and the content follows a 'content:' label.
A 'trailing_newline:' label indicates if the original file ended with a newline.

File Block Structure (conceptual example, not parsable as content):
---PAKTXT_FILE_START-...---
filename: path/to/your/file.go
executable: true
trailing_newline: true
content:
// Your file content here
---PAKTXT_FILE_END-...---

`

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

var (
	workingDirPath string
	versionFlag    bool
	helpFlag       bool
)

var excludedDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "__pycache__": true,
	"build": true, "dist": true, "target": true, ".idea": true,
	".vscode": true, ".cache": true, "tmp": true,
}

type FileBlock struct {
	Filename           string
	IsExecutable       bool
	HasTrailingNewline bool
	Content            []byte
}

func main() {
	rootFlags := flag.NewFlagSet("paktxt", flag.ExitOnError)
	rootFlags.BoolVar(&versionFlag, "version", false, "Show application version")
	rootFlags.BoolVar(&versionFlag, "v", false, "Short for --version")
	rootFlags.BoolVar(&helpFlag, "help", false, "Show this help message")
	rootFlags.BoolVar(&helpFlag, "h", false, "Short for --help")

	packCmd := flag.NewFlagSet("pack", flag.ExitOnError)
	var packToClipboard bool
	var packOutputFile string
	var packExcludePatterns string
	var packFilterPatterns string
	// var packIncludePatterns string // REMOVED: --include flag
	packCmd.BoolVar(&packToClipboard, "clipboard", false, "Pack content to clipboard.")
	packCmd.BoolVar(&packToClipboard, "b", false, "Short for --clipboard.")
	packCmd.StringVar(&packOutputFile, "output-file", "", "Output filename for concatenation.")
	packCmd.StringVar(&packOutputFile, "o", "", "Short for --output-file.")
	packCmd.StringVar(&packExcludePatterns, "exclude", "", "Comma-separated glob patterns for files/paths to exclude (e.g., '*.md,temp/*').")
	packCmd.StringVar(&packExcludePatterns, "e", "", "Short for --exclude.")
	packCmd.StringVar(&packFilterPatterns, "filter", "", "Comma-separated glob patterns to include; only files matching these patterns will be considered.")
	packCmd.StringVar(&packFilterPatterns, "f", "", "Short for --filter.")
	// packCmd.StringVar(&packIncludePatterns, "include", "", "Comma-separated glob patterns to force inclusion. Files matching these patterns will bypass most other exclusion rules (e.g., common binary extensions, byte-signature checks). Use with caution!") // REMOVED
	// packCmd.StringVar(&packIncludePatterns, "i", "", "Short for --include.") // REMOVED
	packCmd.StringVar(&workingDirPath, "working-dir", "", "Specify the directory to operate within instead of the current directory.")
	packCmd.StringVar(&workingDirPath, "w", "", "Short for --working-dir.")
	packCmd.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s pack [flags]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Packs files and outputs to clipboard or a specified file.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		packCmd.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s pack --clipboard            # Pack current directory and copy to clipboard.\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s pack -b                   # Short form of the above.\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s pack --output-file my_project.paktxt # Pack files and write to my_project.paktxt.\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s pack -o my_project.paktxt  # Short form of the above.\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s pack -e '*.log,*.tmp' -o my_project.paktxt # Exclude log/tmp files.\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s pack -f '*.go,*.md' -o my_project.paktxt # Only include Go and Markdown files.\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s pack -e 'node_modules/*' -f '*.js,*.ts' -b # Exclude node_modules but only pack JS/TS files.\n", os.Args[0])
		// fmt.Fprintf(os.Stderr, "  %s pack -i 'my_binary_script' -b # Force inclusion of a specific binary script.\n", os.Args[0]) // REMOVED
		fmt.Fprintf(os.Stderr, "  %s pack -w /path/to/project -b  # Operate in a specific directory.\n", os.Args[0])
	}

	unpackCmd := flag.NewFlagSet("unpack", flag.ExitOnError)
	var unpackFromClipboard bool
	var unpackPaktxtFile string
	var unpackExcludePatterns string
	var unpackFilterPatterns string
	// var unpackIncludePatterns string // REMOVED: --include flag
	unpackCmd.BoolVar(&unpackFromClipboard, "clipboard", false, "Unpack content from clipboard.")
	unpackCmd.BoolVar(&unpackFromClipboard, "b", false, "Short for --clipboard.")
	unpackCmd.StringVar(&unpackPaktxtFile, "paktxt-file", "", "Input .paktxt filename for restoration.")
	unpackCmd.StringVar(&unpackPaktxtFile, "i", "", "Short for --paktxt-file.")
	unpackCmd.StringVar(&unpackExcludePatterns, "exclude", "", "Comma-separated glob patterns for files/paths to exclude from restoration (e.g., 'config.json,*.bak').")
	unpackCmd.StringVar(&unpackExcludePatterns, "e", "", "Short for --exclude.")
	unpackCmd.StringVar(&unpackFilterPatterns, "filter", "", "Comma-separated glob patterns to include; only files matching these patterns will be restored.")
	unpackCmd.StringVar(&unpackFilterPatterns, "f", "", "Short for --filter.")
	// unpackCmd.StringVar(&unpackIncludePatterns, "include", "", "Comma-separated glob patterns to force inclusion during restoration. Files matching these patterns will bypass user-defined --exclude patterns. Use with caution!") // REMOVED
	// unpackCmd.StringVar(&unpackIncludePatterns, "j", "", "Short for --include.") // REMOVED (re-used 'j' from previous change)
	unpackCmd.StringVar(&workingDirPath, "working-dir", "", "Specify the directory to operate within instead of the current directory.")
	unpackCmd.StringVar(&workingDirPath, "w", "", "Short for --working-dir.")
	unpackCmd.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s unpack [flags]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Restores files from clipboard or a specified .paktxt file.\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		unpackCmd.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s unpack --clipboard          # Read from clipboard and restore files.\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s unpack -b                 # Short form of the above.\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s unpack --paktxt-file my_archive.paktxt # Read from my_archive.paktxt and restore files.\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s unpack -i my_archive.paktxt # Short form of the above (input file).\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s unpack -e 'my_secrets.txt,temp_config/*' -b # Unpack from clipboard, excluding sensitive files.\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s unpack -f '*.html,*.css' -b  # Only restore HTML and CSS files.\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s unpack -w /new/location -b  # Operate in a specific directory.\n", os.Args[0])
		// fmt.Fprintf(os.Stderr, "  %s unpack -j 'important_backup.bak' -b # Force restoration of a file that would normally be excluded.\n", os.Args[0]) // REMOVED
	}

	defaultUsage := func() {
		fmt.Fprintf(os.Stderr, "Usage: %s <command> [flags]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "paktxt is a versatile command-line tool to consolidate and restore text-based files.\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  pack    Consolidate files and output (to clipboard or file).\n")
		fmt.Fprintf(os.Stderr, "  unpack  Restore files from input (from clipboard or .paktxt file).\n\n")
		fmt.Fprintf(os.Stderr, "Global Flags:\n")
		rootFlags.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nRun '%s <command> --help' for more information on a command.\n", os.Args[0])
	}

	if len(os.Args) > 1 && (os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "-v" || os.Args[1] == "--version") {
		rootFlags.Parse(os.Args[1:])
	} else {
		rootFlags.Parse(os.Args[1:len(os.Args)])
	}

	if versionFlag {
		fmt.Printf("paktxt %s\n", version)
		os.Exit(0)
	}
	if helpFlag {
		defaultUsage()
		os.Exit(0)
	}

	if len(os.Args) < 2 {
		defaultUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "pack":
		packCmd.Parse(os.Args[2:])
		if packToClipboard && packOutputFile != "" {
			fmt.Fprintf(os.Stderr, "Error: Cannot use --clipboard/-b and --output-file/-o simultaneously with 'pack' command.\n\n")
			packCmd.Usage()
			os.Exit(1)
		}
		if !packToClipboard && packOutputFile == "" {
			fmt.Fprintf(os.Stderr, "Error: 'pack' command requires either --clipboard/-b or --output-file/-o.\n\n")
			packCmd.Usage()
			os.Exit(1)
		}
		// Resolve absolute path for output file before changing working directory
		var absPackOutputFile string
		if packOutputFile != "" {
			var err error
			absPackOutputFile, err = filepath.Abs(packOutputFile)
			if err != nil {
				fmt.Printf("Error resolving absolute path for output file: %v\n", err)
				os.Exit(1)
			}
		}

		if workingDirPath != "" {
			if err := changeWorkingDir(workingDirPath); err != nil {
				os.Exit(1)
			}
		}
		excludePatternsSlice := parsePatterns(packExcludePatterns)
		filterPatternsSlice := parsePatterns(packFilterPatterns)
		// includePatternsSlice := parsePatterns(packIncludePatterns) // REMOVED
		if err := concatenateAndOutput(packToClipboard, absPackOutputFile, excludePatternsSlice, filterPatternsSlice, nil); err != nil { // Pass nil for includePatterns
			fmt.Printf("Error during pack operation: %v\n", err)
			os.Exit(1)
		}
	case "unpack":
		unpackCmd.Parse(os.Args[2:])
		if unpackFromClipboard && unpackPaktxtFile != "" {
			fmt.Fprintf(os.Stderr, "Error: Cannot use --clipboard/-b and --paktxt-file/-i simultaneously with 'unpack' command.\n\n")
			unpackCmd.Usage()
			os.Exit(1)
		}
		if !unpackFromClipboard && unpackPaktxtFile == "" {
			fmt.Fprintf(os.Stderr, "Error: 'unpack' command requires either --clipboard/-b or --paktxt-file/-i.\n\n")
			unpackCmd.Usage()
			os.Exit(1)
		}
		// Resolve absolute path of input file before changing working directory
		if unpackPaktxtFile != "" && !filepath.IsAbs(unpackPaktxtFile) {
			absPath, err := filepath.Abs(unpackPaktxtFile)
			if err != nil {
				fmt.Printf("Error resolving absolute path for input file: %v\n", err)
				os.Exit(1)
			}
			unpackPaktxtFile = absPath
		}
		if workingDirPath != "" {
			if err := changeWorkingDir(workingDirPath); err != nil {
				os.Exit(1)
			}
		}
		excludePatternsSlice := parsePatterns(unpackExcludePatterns)
		filterPatternsSlice := parsePatterns(unpackFilterPatterns)
		// includePatternsSlice := parsePatterns(unpackIncludePatterns) // REMOVED
		if err := restoreFiles(unpackFromClipboard, unpackPaktxtFile, excludePatternsSlice, filterPatternsSlice, nil); err != nil { // Pass nil for includePatterns
			fmt.Printf("Error restoring files: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Files restored successfully.")
	default:
		if !strings.HasPrefix(cmd, "-") {
			fmt.Fprintf(os.Stderr, "Error: Unknown command '%s'.\n\n", cmd)
		} else {
			fmt.Fprintf(os.Stderr, "Error: Invalid flags without a command. Use 'paktxt <command> --help' or 'paktxt --help'.\n\n")
		}
		defaultUsage()
		os.Exit(1)
	}
}

// Renamed from parseExcludePatterns to be more generic for any pattern list
func parsePatterns(patterns string) []string {
	if patterns == "" {
		return nil
	}
	split := strings.Split(patterns, ",")
	var result []string
	for _, p := range split {
		trimmedP := strings.TrimSpace(p)
		if trimmedP != "" {
			result = append(result, trimmedP)
		}
	}
	return result
}

func changeWorkingDir(path string) error {
	absWorkingDir, err := filepath.Abs(path)
	if err != nil {
		fmt.Printf("Error resolving working directory '%s': %v\n", path, err)
		return err
	}
	if err := os.Chdir(absWorkingDir); err != nil {
		fmt.Printf("Error changing working directory to '%s': %v\n", absWorkingDir, err)
		return err
	}
	fmt.Printf("Changed working directory to: %s\n", absWorkingDir)
	return nil
}

func concatenateAndOutput(toClipboard bool, outputFile string, excludePatterns, filterPatterns, includePatterns []string) error {
	fmt.Println("Scanning files for concatenation...")

	var files []string
	var err error

	if isGitRepo() {
		fmt.Println("Git repository detected, scanning current directory recursively (similar to non-git behavior).")
	} else {
		fmt.Println("No Git repository detected. Scanning all files recursively from current directory...")
	}
	// Pass includePatterns as nil or an empty slice if it's no longer used
	files, err = getAllFiles(".", excludePatterns, filterPatterns, nil)
	if err != nil {
		return fmt.Errorf("failed to get file list: %w", err)
	}

	if len(files) == 0 {
		return errors.New("no relevant files found to concatenate")
	}

	files = prioritizeReadme(files)

	paktxtContent, err := buildPaktxtContent(files)
	if err != nil {
		return fmt.Errorf("failed to build paktxt content: %w", err)
	}

	if toClipboard {
		fmt.Println("Attempting to copy content to clipboard...")
		if err := clipboard.WriteAll(paktxtContent); err != nil {
			fmt.Printf("Error: Failed to copy to clipboard: %v\n", err)
			fmt.Println("This might be due to system restrictions or lack of clipboard support.")
			return fmt.Errorf("clipboard copy failed: %w", err)
		}
		fmt.Println("Content successfully copied to clipboard.")
	} else {
		if filepath.Ext(outputFile) == "" {
			outputFile += paktxtExtension
		} else if filepath.Ext(outputFile) != paktxtExtension {
			fmt.Printf("Warning: Output file '%s' does not have a '%s' extension. Using as is.\n", outputFile, paktxtExtension)
		}

		fmt.Printf("Writing content to %s...\n", outputFile)
		if err := os.WriteFile(outputFile, []byte(paktxtContent), 0644); err != nil {
			return fmt.Errorf("failed to write to file %s: %w", outputFile, err)
		}
		fmt.Printf("Content successfully written to %s.\n", outputFile)
	}
	return nil
}

func prioritizeReadme(files []string) []string {
	readmeIndex := -1
	for i, file := range files {
		if strings.EqualFold(filepath.Base(file), "readme.md") {
			readmeIndex = i
			break
		}
	}

	if readmeIndex != -1 {
		readmeFile := files[readmeIndex]
		files = append(files[:readmeIndex], files[readmeIndex+1:]...)
		files = append([]string{readmeFile}, files...)
	}
	return files
}

func restoreFiles(fromClipboard bool, paktxtFile string, excludePatterns, filterPatterns, includePatterns []string) error {
	var paktxtContent string
	var err error

	if fromClipboard {
		fmt.Println("Reading content from clipboard for restoration...")
		paktxtContent, err = clipboard.ReadAll()
		if err != nil {
			fmt.Printf("Error: Failed to read from clipboard: %v\n", err)
			fmt.Println("This might be due to system restrictions or lack of clipboard content.")
			return fmt.Errorf("clipboard read failed: %w", err)
		}
		if paktxtContent == "" {
			fmt.Println("Clipboard content is empty.")
			return errors.New("clipboard content is empty; no parsable paktxt data found")
		}
	} else {
		fmt.Printf("Reading content from file '%s' for restoration...\n", paktxtFile)
		contentBytes, readErr := os.ReadFile(paktxtFile)
		if readErr != nil {
			return fmt.Errorf("failed to read from paktxt file '%s': %w", paktxtFile, readErr)
		}
		paktxtContent = string(contentBytes)
	}

	if paktxtContent == "" {
		return errors.New("input content (from clipboard or file) is empty or contains no parsable paktxt data")
	}

	fmt.Println("Parsing content and restoring files...")
	// Pass includePatterns as nil or an empty slice if it's no longer used
	if err := parseAndRestore(paktxtContent, excludePatterns, filterPatterns, nil); err != nil {
		return fmt.Errorf("failed to parse and restore files: %w", err)
	}
	return nil
}

func isGitRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Stderr = nil
	output, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(output)) == "true"
}

// getAllFiles recursively walks through the directory and collects all non-excluded files.
func getAllFiles(root string, excludePatterns, filterPatterns, includePatterns []string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Always exclude paktxt's own output file name and its extensions.
		// And the executable itself.
		if strings.HasSuffix(strings.ToLower(path), paktxtExtension) ||
			strings.EqualFold(filepath.Base(path), "paktxt") || strings.EqualFold(filepath.Base(path), "paktxt.exe") {
			return nil
		}

		// 1. Directory Exclusion (always first for efficiency)
		if d.IsDir() {
			if shouldExcludeDir(path) {
				return fs.SkipDir
			}
			return nil
		}

		// 2. --filter (Whitelist): If filter patterns are provided, a file *must* match AT LEAST ONE
		//    filter pattern to be considered further. If it doesn't match, it's immediately out.
		if len(filterPatterns) > 0 {
			if !matchesPattern(path, filterPatterns) {
				return nil // Does not match any filter pattern, so exclude
			}
		}

		// 3. (REMOVED: --include logic was here)

		// 4. --exclude (Additive Exclusion): Apply user-defined glob exclusions.
		//    Now applied directly without --include override.
		if matchesPattern(path, excludePatterns) {
			return nil
		}

		// 5. Built-in Path/Extension Exclusion: Checks common system files and extensions.
		//    Now applied directly without --include override.
		if shouldExcludePath(path) {
			return nil
		}

		// 6. Binary Signature Check: Most expensive check, performed last.
		//    Now applied directly without --include override.
		if isBinary, err := isBinaryFileBySignature(path); isBinary {
			fmt.Printf("Skipping binary file (by signature): %s\n", path)
			return nil
		} else if err != nil {
			// If there's an error reading the signature (e.g., permissions), we'll print a warning
			// but still include the file unless we explicitly want to skip on error.
			fmt.Printf("Warning: Error checking binary signature for %s: %v\n", path, err)
		}

		// If not excluded by any of the above, add it.
		relPath, err := filepath.Rel(root, path)
		if err != nil {
			fmt.Printf("Warning: Could not get relative path for %s: %v\n", path, err)
			files = append(files, path)
		} else {
			files = append(files, relPath)
		}
		return nil
	})
	return files, err
}

// shouldExcludeDir checks if a directory should be excluded from scanning.
func shouldExcludeDir(path string) bool {
	dirName := filepath.Base(path)
	return excludedDirs[dirName]
}

// shouldExcludePath checks if a file path indicates it should be excluded based on name or common extension.
// This is the FASTEST check as it doesn't involve opening the file.
func shouldExcludePath(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))

	// Exclude by specific common names (regardless of extension).
	excludedNames := map[string]bool{
		".ds_store":   true, // macOS desktop services store file
		"thumbs.db":   true, // Windows thumbnail cache
		"desktop.ini": true, // Windows desktop customization file
		".localized":  true, // macOS localization marker
		"icon\r":      true, // macOS custom icon file (has a carriage return in name)
		// Add other common system/temp files without extensions here if needed
	}
	if excludedNames[name] {
		return true
	}

	// Exclude by common binary/non-text extensions.
	// This list is intentionally broad to catch files quickly by their extension.
	excludedExtensions := map[string]bool{
		".exe": true, ".dll": true, ".so": true, ".dylib": true, // Executables/Libraries
		".zip": true, ".tar": true, ".gz": true, ".rar": true, ".7z": true, // Archives
		".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".bmp": true, ".svg": true, // Images
		".ico": true,                             // Icons
		".mp3": true, ".wav": true, ".ogg": true, // Audio
		".mp4": true, ".avi": true, ".mov": true, ".mkv": true, // Video
		".pdf":    true,                                // PDF documents
		".sqlite": true, ".db": true, ".sqlite3": true, // Databases
		".log":          true, // Logs are text but often very large and unwanted
		".bin":          true, // Generic binary files
		".class":        true, // Java compiled classes
		".jar":          true, // Java archives (are zips)
		".lock":         true, // Generic lock files
		paktxtExtension: true, // Exclude paktxt's own output
		// Add other extensions that are definitely not text and you don't want to pack
		".obj": true, ".lib": true, ".a": true, // Compiled objects/static libraries
		".dat": true,               // Generic data file, often binary
		".tmp": true,               // Temporary files
		".bak": true,               // Backup files
		".swp": true, ".swo": true, // Vim swap files
		".pyc":     true,                     // Python compiled bytecode
		".iml":     true,                     // IntelliJ IDEA module file (XML, but often auto-generated and noisy)
		".project": true, ".classpath": true, // Eclipse project files (XML, similarly noisy)
		".vspscc": true, ".vssscc": true, // Visual Studio Source Control files
		".suo": true, ".user": true, // Visual Studio user-specific settings
		".ncb": true, ".sdf": true, ".ipch": true, // Visual Studio Intellisense/Browse info
	}

	if excludedExtensions[ext] {
		return true
	}

	// Also, check if any component of the path (directory name) is in `excludedDirs`.
	// This helps catch cases like `project/vendor/somefile.txt` if `vendor` is in excludedDirs.
	// This is a bit redundant with the `fs.SkipDir` in WalkDir, but adds robustness.
	// We check for `filepath.Separator` on both sides to avoid partial matches (e.g., "mybuild" matching "build").
	pathComponents := strings.Split(strings.ToLower(path), string(filepath.Separator))
	for _, comp := range pathComponents {
		if excludedDirs[comp] {
			return true
		}
	}

	return false
}

// isBinaryFileBySignature checks if a file is a binary based on its magic number (file signature).
// It reads only a small prefix of the file for efficiency,
// and acts as a fallback for files that don't have typical binary extensions
// but are, in fact, binary (e.g., executables without extensions, or compressed archives
// used as "dot files" or temp files).
func isBinaryFileBySignature(filePath string) (bool, error) {
	file, err := os.Open(filePath)
	if err != nil {
		// If we can't open it (e.g., permissions), return an error.
		// The caller decides whether to skip or log a warning.
		return false, fmt.Errorf("cannot open file to check signature %s: %w", filePath, err)
	}
	defer file.Close()

	// Read enough bytes to cover most common magic numbers and initial header structures (e.g., PE offset)
	const readBufferSize = 256 // A larger buffer is safer for complex headers like PE
	buffer := make([]byte, readBufferSize)
	n, readErr := io.ReadAtLeast(file, buffer, 4) // Read at least 4 bytes for most simple magic numbers

	if readErr != nil && readErr != io.EOF {
		// If there's a real read error (not just EOF because file is too short), report it.
		return false, fmt.Errorf("failed to read file header for %s: %w", filePath, readErr)
	}
	if n < 4 {
		// File is too small to have common magic numbers, assume it's text (or empty)
		return false, nil
	}

	// --- Check for common executable magic numbers ---
	// ELF: 0x7F 'E' 'L' 'F'
	if n >= 4 && bytes.HasPrefix(buffer, []byte{0x7F, 0x45, 0x4C, 0x46}) {
		return true, nil
	}

	// Mach-O (macOS/iOS executables and libraries)
	// 32-bit big-endian: FEEDFACE
	// 32-bit little-endian: CEFAEDFE
	// 64-bit big-endian: FEEDFACF
	// 64-bit little-endian: CFFAEDFE
	if n >= 4 && (bytes.HasPrefix(buffer, []byte{0xFE, 0xED, 0xFA, 0xCE}) ||
		bytes.HasPrefix(buffer, []byte{0xCE, 0xFA, 0xED, 0xFE}) ||
		bytes.HasPrefix(buffer, []byte{0xFE, 0xED, 0xFA, 0xCF}) ||
		bytes.HasPrefix(buffer, []byte{0xCF, 0xFA, 0xED, 0xFE})) {
		return true, nil
	}

	// PE (Windows Executables: EXE, DLL)
	// Starts with 'MZ' (0x4D 0x5A)
	// Then, at offset 0x3C, there's a 4-byte little-endian pointer to the PE header.
	// The PE header itself starts with 'PE\0\0' (0x50 0x45 0x00 0x00).
	if n >= 2 && bytes.HasPrefix(buffer, []byte{0x4D, 0x5A}) { // Check for 'MZ'
		if n >= 0x3C+4 { // Ensure buffer is large enough to read the PE header offset
			// Read the 4-byte little-endian offset
			peHeaderOffset := uint32(buffer[0x3C]) | uint32(buffer[0x3C+1])<<8 |
				uint32(buffer[0x3C+2])<<16 | uint32(buffer[0x3C+3])<<24

			// Check if the PE header itself is within our buffer
			if int(peHeaderOffset)+4 <= n {
				if bytes.HasPrefix(buffer[peHeaderOffset:], []byte{0x50, 0x45, 0x00, 0x00}) {
					return true, nil // Confirmed PE executable
				}
			}
		}
	}

	// --- Check for common archive/compressed file magic numbers ---
	// ZIP archive (including JAR, WAR, DOCX, XLSX, PPTX, etc. as they are ZIPs)
	if n >= 4 && (bytes.HasPrefix(buffer, []byte{0x50, 0x4B, 0x03, 0x04}) || // Local file header
		bytes.HasPrefix(buffer, []byte{0x50, 0x4B, 0x05, 0x06}) || // Empty archive (central directory end)
		bytes.HasPrefix(buffer, []byte{0x50, 0x4B, 0x07, 0x08})) { // Spanned archive
		return true, nil
	}

	// Gzip compressed file
	if n >= 2 && bytes.HasPrefix(buffer, []byte{0x1F, 0x8B}) {
		return true, nil
	}

	// 7-Zip archive
	if n >= 6 && bytes.HasPrefix(buffer, []byte{0x37, 0x7A, 0xBC, 0xAF, 0x27, 0x1C}) {
		return true, nil
	}

	// --- Check for common database files ---
	// SQLite 3.x database file
	if n >= 16 && bytes.HasPrefix(buffer, []byte{
		0x53, 0x51, 0x4C, 0x69, 0x74, 0x65, 0x20, 0x66,
		0x6F, 0x72, 0x6D, 0x61, 0x74, 0x20, 0x33, 0x00}) {
		return true, nil
	}

	// --- Check for other common non-text files that might not have extensions or have generic ones ---
	// PNG (added here as a definitive non-text check, even if extension usually catches it)
	if n >= 8 && bytes.HasPrefix(buffer, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}) {
		return true, nil
	}
	// JPEG (added here as a definitive non-text check)
	if n >= 4 && (bytes.HasPrefix(buffer, []byte{0xFF, 0xD8, 0xFF, 0xE0}) || // JFIF
		bytes.HasPrefix(buffer, []byte{0xFF, 0xD8, 0xFF, 0xE1})) { // EXIF
		return true, nil
	}
	// GIF (added here as a definitive non-text check)
	if n >= 6 && (bytes.HasPrefix(buffer, []byte{0x47, 0x49, 0x46, 0x38, 0x37, 0x61}) || // GIF87a
		bytes.HasPrefix(buffer, []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61})) { // GIF89a
		return true, nil
	}
	// BMP (added here as a definitive non-text check)
	if n >= 2 && bytes.HasPrefix(buffer, []byte{0x42, 0x4D}) { // 'BM'
		return true, nil
	}

	// PDF (added here as a definitive non-text check, often starts with %PDF)
	if n >= 4 && bytes.HasPrefix(buffer, []byte{0x25, 0x50, 0x44, 0x46}) { // %PDF
		return true, nil
	}

	// If none of the above magic numbers match, assume it's not a specific known binary type.
	return false, nil
}

// matchesPattern checks if a file path matches any of the provided glob patterns.
// It returns true if it matches at least one pattern, false otherwise.
func matchesPattern(filePath string, patterns []string) bool {
	for _, pattern := range patterns {
		// Check against base name (e.g., "*.log")
		matched, err := filepath.Match(pattern, filepath.Base(filePath))
		if err != nil {
			fmt.Printf("Warning: Invalid glob pattern '%s': %v\n", pattern, err)
			continue
		}
		if matched {
			return true
		}

		// Check against full path (e.g., "temp/*")
		matchedFullPath, err := filepath.Match(pattern, filePath)
		if err != nil {
			fmt.Printf("Warning: Invalid glob pattern '%s': %v\n", pattern, err)
			continue
		}
		if matchedFullPath {
			return true
		}
	}
	return false
}

func buildPaktxtContent(files []string) (string, error) {
	var builder strings.Builder
	builder.WriteString(paktxtHeader)

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			fmt.Printf("Warning: Could not read file %s: %v\n", file, err)
			continue
		}

		contentBytes := content
		if bytes.HasPrefix(contentBytes, utf8BOM) {
			contentBytes = contentBytes[len(utf8BOM):]
		}

		// This check is very important to prevent infinite recursion if a paktxt output is scanned.
		// It's still here as a safeguard, although getAllFiles also tries to filter it by name/extension.
		if bytes.HasPrefix(contentBytes, []byte(paktxtHeader)) {
			fmt.Printf("Skipping file %s as it appears to be a paktxt output.\n", file)
			continue
		}

		fileInfo, err := os.Stat(file)
		isExecutable := false
		if err == nil {
			isExecutable = (fileInfo.Mode().Perm()&0111 != 0)
		} else {
			fmt.Printf("Warning: Could not get file info for %s: %v. Assuming non-executable.\n", file, err)
		}

		hasTrailingNewline := false
		if len(content) > 0 {
			lastByte := content[len(content)-1]
			if lastByte == '\n' {
				hasTrailingNewline = true // Found a trailing newline
				if len(content) > 1 && content[len(content)-2] == '\r' {
					// This is a \r\n ending, still considered a trailing newline
				}
			}
		}

		builder.WriteString(startBlockDelimiter)
		builder.WriteString("\n")
		builder.WriteString(filenameLabel)
		builder.WriteString(file)
		builder.WriteString("\n")
		builder.WriteString(executableLabel)
		if isExecutable {
			builder.WriteString("true")
		} else {
			builder.WriteString("false")
		}
		builder.WriteString("\n")
		builder.WriteString(trailingNewlineLabel)
		if hasTrailingNewline {
			builder.WriteString("true")
		} else {
			builder.WriteString("false")
		}
		builder.WriteString("\n")
		builder.WriteString(contentLabel)
		// Ensure exactly one newline separates the content and the end delimiter.
		// If the original content didn't end with a newline, add one here.
		builder.Write(content)
		if !hasTrailingNewline {
			builder.WriteString("\n")
		}
		builder.WriteString(endBlockDelimiter)
		builder.WriteString("\n") // Add an extra newline after the end delimiter for block separation
	}
	return builder.String(), nil
}

// parseAndRestore parses the paktxt content and recreates files and directories.
func parseAndRestore(paktxtContent string, excludePatterns, filterPatterns, includePatterns []string) error {
	paktxtBytes := []byte(paktxtContent)
	cursor := 0 // Current position in paktxtBytes

	// Simple header skip: Find the first occurrence of the start delimiter.
	headerEndIndex := bytes.Index(paktxtBytes, []byte(startBlockDelimiter))
	if headerEndIndex == -1 {
		return errors.New("no file blocks found in paktxt content (missing start delimiter)")
	}
	cursor = headerEndIndex // Start parsing from the first delimiter

	for cursor < len(paktxtBytes) {
		startBlockIdx := bytes.Index(paktxtBytes[cursor:], []byte(startBlockDelimiter))
		if startBlockIdx == -1 {
			break // No more start delimiters found, we are done.
		}

		cursor += startBlockIdx + len(startBlockDelimiter)
		// Skip the newline(s) after the start delimiter
		if cursor < len(paktxtBytes) && paktxtBytes[cursor] == '\n' {
			cursor++
		}
		if cursor < len(paktxtBytes) && paktxtBytes[cursor] == '\r' { // Handle CRLF
			cursor++
			if cursor < len(paktxtBytes) && paktxtBytes[cursor] == '\n' {
				cursor++
			}
		}

		currentFileBlock := &FileBlock{}
		foundContentLabel := false

		for {
			lineEnd := bytes.IndexByte(paktxtBytes[cursor:], '\n')
			if lineEnd == -1 {
				return errors.New("malformed paktxt content: unexpected end of data during metadata parsing")
			}

			lineBytes := bytes.TrimSuffix(paktxtBytes[cursor:cursor+lineEnd], []byte("\r"))
			line := string(lineBytes)

			lineAdvance := lineEnd + 1
			if cursor+lineAdvance > len(paktxtBytes) {
				return errors.New("malformed paktxt content: reading past end of buffer")
			}

			if strings.HasPrefix(line, filenameLabel) {
				currentFileBlock.Filename = strings.TrimPrefix(line, filenameLabel)
			} else if strings.HasPrefix(line, executableLabel) {
				execStr := strings.TrimPrefix(line, executableLabel)
				currentFileBlock.IsExecutable = (execStr == "true")
			} else if strings.HasPrefix(line, trailingNewlineLabel) {
				tnlStr := strings.TrimPrefix(line, trailingNewlineLabel)
				currentFileBlock.HasTrailingNewline = (tnlStr == "true")
			} else if strings.HasPrefix(line, contentLabel[:len(contentLabel)-1]) {
				foundContentLabel = true
				lineAdvance = len(contentLabel)
			} else if strings.TrimSpace(line) == "" {
				// Allow empty lines in metadata
			} else {
				fmt.Printf("Warning: Unexpected line in metadata block for file %q: %q\n", currentFileBlock.Filename, line)
			}

			cursor += lineAdvance

			if foundContentLabel {
				break
			}
		}

		endBlockIdx := bytes.Index(paktxtBytes[cursor:], []byte(endBlockDelimiter))
		if endBlockIdx == -1 {
			return errors.New("malformed paktxt content: missing end delimiter for file block")
		}

		currentFileBlock.Content = paktxtBytes[cursor : cursor+endBlockIdx]
		cursor += endBlockIdx + len(endBlockDelimiter)

		if cursor < len(paktxtBytes) && paktxtBytes[cursor] == '\n' {
			cursor++
			if cursor < len(paktxtBytes) && paktxtBytes[cursor] == '\r' {
				cursor++
				if cursor < len(paktxtBytes) && paktxtBytes[cursor] == '\n' {
					cursor++
				}
			}
		}
		if cursor < len(paktxtBytes) && paktxtBytes[cursor] == '\n' {
			cursor++
		}

		if currentFileBlock == nil || currentFileBlock.Filename == "" {
			fmt.Println("Warning: Skipping malformed file block (no filename found).")
			continue
		}

		// Apply filter patterns during restore: If filter patterns are present, the file must match.
		if len(filterPatterns) > 0 {
			if !matchesPattern(currentFileBlock.Filename, filterPatterns) {
				fmt.Printf("Skipping restoration of filtered file: %s\n", currentFileBlock.Filename)
				continue
			}
		}

		// (REMOVED: --include logic was here)

		// Apply user-defined exclude patterns during restore.
		if matchesPattern(currentFileBlock.Filename, excludePatterns) {
			fmt.Printf("Skipping restoration of excluded file: %s (due to --exclude)\n", currentFileBlock.Filename)
			continue
		}

		dir := filepath.Dir(currentFileBlock.Filename)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create directory '%s' for file '%s': %w", dir, currentFileBlock.Filename, err)
			}
		}

		// If the original file did NOT have a trailing newline, remove the one added during packing.
		contentLen := len(currentFileBlock.Content)
		if !currentFileBlock.HasTrailingNewline && contentLen > 0 {
			// Check for and remove trailing CRLF (\r\n) first
			if contentLen >= 2 && currentFileBlock.Content[contentLen-2] == '\r' && currentFileBlock.Content[contentLen-1] == '\n' {
				currentFileBlock.Content = currentFileBlock.Content[:contentLen-2]
			} else if currentFileBlock.Content[contentLen-1] == '\n' {
				// If not CRLF, check for and remove single LF (\n)
				currentFileBlock.Content = currentFileBlock.Content[:len(currentFileBlock.Content)-1]

			}
		}
		if err := os.WriteFile(currentFileBlock.Filename, currentFileBlock.Content, os.FileMode(0644)); err != nil {
			return fmt.Errorf("failed to write file '%s': %w", currentFileBlock.Filename, err)
		}
		fmt.Printf("Restored: %s\n", currentFileBlock.Filename)

		if currentFileBlock.IsExecutable {
			if err := os.Chmod(currentFileBlock.Filename, os.FileMode(0755)); err != nil {
				fmt.Printf("Warning: Failed to set executable permission for '%s': %v\n", currentFileBlock.Filename, err)
			}
		}
	}

	return nil
}
