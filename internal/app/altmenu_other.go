//go:build !windows

package app

import "github.com/wailsapp/wails/v3/pkg/application"

func guardAltMenu(*application.Options) {}
