package service

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

var testKey = []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nclave\n-----END OPENSSH PRIVATE KEY-----\n")

func TestAC9_KeyStoreReturnsKey(t *testing.T) {
	store := NewKeyStore(time.Minute)
	store.Put("vm1", testKey)

	got, err := store.Take("vm1")
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if !bytes.Equal(got, testKey) {
		t.Errorf("Take = %q, se esperaba %q", got, testKey)
	}
}

func TestAC10_KeyStoreSecondTakeFails(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(store *KeyStore)
	}{
		{"segunda descarga", func(store *KeyStore) {
			store.Put("vm1", testKey)
			if _, err := store.Take("vm1"); err != nil {
				t.Fatalf("primer Take: %v", err)
			}
		}},
		{"nunca generada", func(*KeyStore) {}},
		{"descartada", func(store *KeyStore) {
			store.Put("vm1", testKey)
			store.Discard("vm1")
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := NewKeyStore(time.Minute)
			tc.prepare(store)

			got, err := store.Take("vm1")
			if !errors.Is(err, ErrKeyUnavailable) {
				t.Errorf("err = %v, se esperaba ErrKeyUnavailable", err)
			}
			if got != nil {
				t.Errorf("Take = %q, no se esperaba llave", got)
			}
		})
	}
}

func TestAC11_KeyStoreExpires(t *testing.T) {
	t.Run("vence tras el TTL", func(t *testing.T) {
		store := NewKeyStore(20 * time.Millisecond)
		store.Put("vm1", testKey)

		time.Sleep(50 * time.Millisecond)

		if _, err := store.Take("vm1"); !errors.Is(err, ErrKeyUnavailable) {
			t.Errorf("err = %v, se esperaba ErrKeyUnavailable", err)
		}
	})

	t.Run("el temporizador de una llave anterior no borra la nueva", func(t *testing.T) {
		store := NewKeyStore(40 * time.Millisecond)
		store.Put("vm1", []byte("vieja"))
		time.Sleep(25 * time.Millisecond)
		store.Put("vm1", testKey) // regenerar reinicia los 10 minutos
		time.Sleep(25 * time.Millisecond)

		got, err := store.Take("vm1")
		if err != nil {
			t.Fatalf("Take: %v", err)
		}
		if !bytes.Equal(got, testKey) {
			t.Errorf("Take = %q, se esperaba la llave nueva", got)
		}
	})
}
