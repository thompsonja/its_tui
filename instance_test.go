package main

import "testing"

func TestStatusLine_NoInstance(t *testing.T) {
	inst := Instance{}
	if inst.StatusLine() != "no instance selected" {
		t.Fatalf("unexpected: %q", inst.StatusLine())
	}
}

func TestStatusLine_WithName(t *testing.T) {
	inst := Instance{Name: "hello-world"}
	if inst.StatusLine() != "hello-world" {
		t.Fatalf("unexpected: %q", inst.StatusLine())
	}
}
