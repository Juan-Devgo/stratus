package service

import (
	"sync"
	"time"
)

// KeyStore guarda en memoria la llave privada del usuario de cada VM hasta su única descarga
// o hasta que vence su tiempo de vida, lo que ocurra primero (REQ-8, P5). Nunca se escribe en
// disco, en el historial ni en el log.
type KeyStore struct {
	ttl     time.Duration
	mu      sync.Mutex
	pending map[string]*pendingKey
}

// pendingKey es una llave en espera de descarga con su temporizador de vencimiento.
type pendingKey struct {
	key   []byte
	timer *time.Timer
}

func NewKeyStore(ttl time.Duration) *KeyStore {
	return &KeyStore{ttl: ttl, pending: map[string]*pendingKey{}}
}

// Put guarda la llave de la VM y reemplaza la anterior si la había. Tras ttl se descarta sola.
func (k *KeyStore) Put(vm string, key []byte) {
	entry := &pendingKey{key: key}

	k.mu.Lock()
	defer k.mu.Unlock()
	k.removeLocked(vm)
	k.pending[vm] = entry
	entry.timer = time.AfterFunc(k.ttl, func() {
		k.mu.Lock()
		defer k.mu.Unlock()
		// Solo se descarta esta entrada: si la llave se regeneró, la nueva tiene su propio plazo.
		if k.pending[vm] == entry {
			k.removeLocked(vm)
		}
	})
}

// Take entrega la llave y la quita de memoria: una segunda llamada devuelve ErrKeyUnavailable.
func (k *KeyStore) Take(vm string) ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	entry, ok := k.pending[vm]
	if !ok {
		return nil, ErrKeyUnavailable
	}
	entry.timer.Stop()
	delete(k.pending, vm)
	return entry.key, nil
}

// Discard borra la llave pendiente de la VM, si existe (por ejemplo, al eliminar la VM).
func (k *KeyStore) Discard(vm string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.removeLocked(vm)
}

// removeLocked detiene el temporizador, sobrescribe la llave con ceros para no dejarla en
// memoria más de lo necesario y la quita del mapa. Requiere k.mu tomado.
func (k *KeyStore) removeLocked(vm string) {
	entry, ok := k.pending[vm]
	if !ok {
		return
	}
	entry.timer.Stop()
	clear(entry.key)
	delete(k.pending, vm)
}
