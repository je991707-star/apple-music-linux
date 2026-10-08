package main

import (
	"sync"
	"testing"
)

func TestVLCGate(t *testing.T) {
	var g vlcGate
	if g.owns("") || g.owns("a") {
		t.Fatal("fresh gate must own nothing")
	}
	g.set("a")
	if !g.owns("a") || g.owns("b") || g.owns("") {
		t.Fatal("only the current session may drive the player")
	}
	g.set("b")
	if g.owns("a") {
		t.Fatal("a superseded session must be rejected")
	}
	g.clear()
	if g.owns("b") {
		t.Fatal("cleared gate must own nothing")
	}
}

func TestVLCGateConcurrent(t *testing.T) {
	var g vlcGate
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); g.set("x") }()
		go func() { defer wg.Done(); _ = g.owns("x") }()
	}
	wg.Wait()
}

func TestParseSeekSeconds(t *testing.T) {
	for in, want := range map[string]bool{
		"0": true, "12.5": true, "1e3": true,
		"": false, "-1": false, "NaN": false, "nan": false, "Inf": false, "+Inf": false, "-Inf": false, "abc": false,
	} {
		if _, ok := parseSeekSeconds(in); ok != want {
			t.Errorf("parseSeekSeconds(%q) ok=%v, want %v", in, ok, want)
		}
	}
}

func TestCatalogIDRe(t *testing.T) {
	for in, want := range map[string]bool{
		"1440935467": true, "pl.u-abc123": true, "p.xyz": true, "i.AbC_9": true, "us": true, "a..b": true,
		"": false, ".": false, "..": false, ".hidden": false, "a/b": false, "a?b": false, "a#b": false, "a b": false,
	} {
		if got := catalogIDRe.MatchString(in); got != want {
			t.Errorf("catalogIDRe(%q) = %v, want %v", in, got, want)
		}
	}
}
