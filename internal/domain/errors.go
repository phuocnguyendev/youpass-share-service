package domain

import "errors"

var (
	ErrLinkNotFound        = errors.New("share link not found")
	ErrLinkGone            = errors.New("share link disabled, deleted or expired")
	ErrForbidden           = errors.New("forbidden")
	ErrInvalidResourceType = errors.New("invalid resource type")
	ErrInvalidStatus       = errors.New("invalid status")
	ErrSubmissionNotFound  = errors.New("submission not found")
)
