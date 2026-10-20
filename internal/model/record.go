// Package model holds the shared types describing a scanned media file and
// its classification outcome, independent of how it was discovered,
// classified or relocated.
package model

import "time"

// MediaType identifies what kind of media a file was recognized as.
type MediaType string

const (
	MediaImage       MediaType = "Image"
	MediaVideo       MediaType = "Video"
	MediaUnsupported MediaType = "Unsupported"
)

// Bucket is the content-risk category a file was scored into.
type Bucket string

const (
	BucketSafe       Bucket = "Safe"
	BucketSuggestive Bucket = "Suggestive"
	BucketExplicit   Bucket = "Explicit"
	// BucketNone applies when no score was computed (unsupported file,
	// error, excluded, symlink).
	BucketNone Bucket = ""
)

// Status describes what happened to a single source file.
type Status string

const (
	// StatusMoved means the file was relocated to -out (Explicit bucket,
	// high-confidence score): the source no longer has it.
	StatusMoved Status = "Moved"
	// StatusCopiedForReview means the file was copied to -out for a human
	// to look at (Suggestive bucket, low-confidence score): the source is
	// untouched.
	StatusCopiedForReview Status = "Copied - For Review"
	// StatusLeftInPlace means the file was classified Safe and never
	// touched.
	StatusLeftInPlace    Status = "Left In Place"
	StatusSkippedSymlink Status = "Skipped - Symlink"
	StatusExcluded       Status = "Skipped - Excluded"
	StatusFailed         Status = "Failed"
)

// Record is one row of the final report: a single source file and what
// happened to it.
type Record struct {
	SourcePath string
	DestPath   string
	FileName   string
	MediaType  MediaType
	Bucket     Bucket
	// Score is the model's probability (0-1) that the file contains
	// explicit content. For video, this is the maximum score across all
	// sampled frames. Zero/unset when no classification occurred.
	Score float64
	// SamplesChecked is the number of frames sampled and classified, for
	// video files only (0 for images and everything else).
	SamplesChecked int
	Status         Status
	Reason         string
	SizeBytes      int64
	Timestamp      time.Time
}
