package main

import (
	"testing"
)

func TestHasList(t *testing.T) {
	l := []string{"hei", "du", "der"}
	if !hasl(l, "hei") {
		t.Error("Wrong: List has \"hei\"")
	}
	if hasl(l, "hello") {
		t.Error("Wrong: List does not have \"hello\"")
	}
}

func TestHasGlob(t *testing.T) {
	l := []string{"hei", "du", "der", "kake*", "*ake*"}
	if !hasglob(l, "hei") {
		t.Error("Wrong: List should match \"hei\"")
	}
	if hasglob(l, "hello") {
		t.Error("Wrong: List should not match \"hello\"")
	}
	if !hasglob(l, "kakeball") {
		t.Error("Wrong: List should match \"kakeball\"")
	}
	if !hasglob(l, "kake") {
		t.Error("Wrong: List should match \"kake\"")
	}
	if !hasglob(l, "kakeeeeee") {
		t.Error("Wrong: List should match \"kakeeeeee\"")
	}
	if !hasglob(l, "pepperkake") {
		t.Error("Wrong: List should match \"kakeeeeee\"")
	}
}
