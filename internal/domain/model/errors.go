package model

import "errors"

var (
	// ErrInvalidProfile はプロフィール設定が不正であることを示す。
	ErrInvalidProfile = errors.New("invalid profile")

	// ErrInvalidSource はソース設定が不正であることを示す。
	ErrInvalidSource = errors.New("invalid source config")

	// ErrUnknownSource は指定されたソースが設定に存在しないことを示す。
	ErrUnknownSource = errors.New("unknown source")

	// ErrMissingWebhookURL は実送信に必要な Webhook URL が未設定であることを示す。
	ErrMissingWebhookURL = errors.New("missing webhook url")
)
