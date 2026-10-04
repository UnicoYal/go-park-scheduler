package auth

import (
	"path/filepath"
	"testing"
)

func TestStoreCanOnlyBeClaimedOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "auth.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Claim(100); err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	if ok, err := s.Claim(200); err != nil || ok {
		t.Fatalf("second claim: ok=%v err=%v", ok, err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reopened.Owner(); got != 100 {
		t.Fatalf("owner=%d, want 100", got)
	}
	if err := reopened.SetTarget(100, -500); err != nil {
		t.Fatal(err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Target(); got != -500 {
		t.Fatalf("target=%d, want -500", got)
	}
	if known := again.KnownChats(); len(known) != 1 || known[0] != -500 {
		t.Fatalf("known chats=%v, want [-500]", known)
	}
	if delivery := again.DeliveryChats(); len(delivery) != 1 || delivery[0] != -500 {
		t.Fatalf("delivery chats=%v, want [-500]", delivery)
	}
	if err := again.SetTarget(200, -600); err == nil {
		t.Fatal("another user replaced the target chat")
	}
	if err := again.ResetOwner(); err != nil {
		t.Fatal(err)
	}
	resetStore, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if resetStore.Owner() != 0 || len(resetStore.DeliveryChats()) != 1 {
		t.Fatalf("reset lost state: owner=%d delivery=%v", resetStore.Owner(), resetStore.DeliveryChats())
	}
	if ok, err := resetStore.Claim(200); err != nil || !ok {
		t.Fatalf("new owner claim: ok=%v err=%v", ok, err)
	}
	again = resetStore
	if err := again.ClearTarget(-500); err != nil || again.Target() != 0 {
		t.Fatalf("clear target: target=%d err=%v", again.Target(), err)
	}
	if err := again.UnregisterChat(-500); err != nil || len(again.KnownChats()) != 0 {
		t.Fatalf("unregister chat: known=%v err=%v", again.KnownChats(), err)
	}
	if len(again.DeliveryChats()) != 0 {
		t.Fatalf("delivery chat was not removed: %v", again.DeliveryChats())
	}
}
