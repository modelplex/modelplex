package main

import (
	"testing"

	"github.com/jessevdk/go-flags"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptions_DefaultValues(t *testing.T) {
	var opts Options
	parser := flags.NewParser(&opts, flags.Default)

	args := []string{}
	_, err := parser.ParseArgs(args)
	require.NoError(t, err)

	assert.Equal(t, "config.toml", opts.Config)
	assert.Equal(t, "", opts.Socket) // Socket is now optional, empty by default
	assert.Equal(t, ":11435", opts.HTTP) // New default HTTP address
	assert.False(t, opts.Verbose)
	assert.False(t, opts.Version)
}

func TestOptions_CustomValues(t *testing.T) {
	var opts Options
	parser := flags.NewParser(&opts, flags.Default)

	args := []string{
		"--config", "/custom/config.toml",
		"--socket", "/tmp/custom.socket",
		"--http", "0.0.0.0:8080",
		"--verbose",
	}
	_, err := parser.ParseArgs(args)
	require.NoError(t, err)

	assert.Equal(t, "/custom/config.toml", opts.Config)
	assert.Equal(t, "/tmp/custom.socket", opts.Socket)
	assert.Equal(t, "0.0.0.0:8080", opts.HTTP)
	assert.True(t, opts.Verbose)
	assert.False(t, opts.Version)
}

func TestOptions_ShortFlags(t *testing.T) {
	var opts Options
	parser := flags.NewParser(&opts, flags.Default)

	args := []string{
		"-c", "short.toml",
		"-s", "short.socket",
		"--http", "127.0.0.1:9090",
		"-v",
	}
	_, err := parser.ParseArgs(args)
	require.NoError(t, err)

	assert.Equal(t, "short.toml", opts.Config)
	assert.Equal(t, "short.socket", opts.Socket)
	assert.Equal(t, "127.0.0.1:9090", opts.HTTP)
	assert.True(t, opts.Verbose)
}

func TestOptions_VersionFlag(t *testing.T) {
	var opts Options
	parser := flags.NewParser(&opts, flags.Default)

	args := []string{"--version"}
	_, err := parser.ParseArgs(args)
	require.NoError(t, err)

	assert.True(t, opts.Version)
}

func TestOptions_HelpFlag(t *testing.T) {
	var opts Options
	parser := flags.NewParser(&opts, flags.Default)

	args := []string{"--help"}
	_, err := parser.ParseArgs(args)

	// Help should return an error of type ErrHelp
	require.Error(t, err)
	flagsErr, ok := err.(*flags.Error)
	require.True(t, ok)
	assert.Equal(t, flags.ErrHelp, flagsErr.Type)
}

func TestOptions_InvalidFlag(t *testing.T) {
	var opts Options
	parser := flags.NewParser(&opts, flags.Default)

	args := []string{"--invalid-flag"}
	_, err := parser.ParseArgs(args)

	require.Error(t, err)
	flagsErr, ok := err.(*flags.Error)
	require.True(t, ok)
	assert.Equal(t, flags.ErrUnknownFlag, flagsErr.Type)
}

func TestVersionVariables(t *testing.T) {
	// Test that version variables are defined
	assert.NotEmpty(t, version)
	assert.NotEmpty(t, commit)
	assert.NotEmpty(t, date)
}

func TestOptionsStructTags(t *testing.T) {
	// Verify struct tags are correctly defined using reflection
	var opts Options
	parser := flags.NewParser(&opts, flags.Default)

	// This validates the struct tags by attempting to parse help
	args := []string{"--help"}
	_, err := parser.ParseArgs(args)

	// Should get help error, not parsing error
	require.Error(t, err)
	flagsErr, ok := err.(*flags.Error)
	require.True(t, ok)
	assert.Equal(t, flags.ErrHelp, flagsErr.Type)
}
