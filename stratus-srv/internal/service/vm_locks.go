package service

import "sync"

// VMLocks permite una sola operación por VM a la vez (REQ-15).
// Una segunda petición se rechaza de inmediato; no hay cola de espera.
// El valor cero está listo para usarse.
type VMLocks struct {
	mu     sync.Mutex
	busy   map[string]bool
	create bool // bloqueo global de creación: lo comparten create_vm y create_lb (AC-8)
}

// TryLock marca la VM como ocupada y devuelve true, o devuelve false si ya lo estaba.
func (l *VMLocks) TryLock(vm string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.busy == nil {
		l.busy = make(map[string]bool)
	}
	if l.busy[vm] {
		return false
	}
	l.busy[vm] = true
	return true
}

// Unlock libera la VM al terminar la operación.
func (l *VMLocks) Unlock(vm string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.busy, vm)
}

// TryLockAll bloquea todas las VMs o ninguna (REQ-21): si alguna está ocupada no toma ninguna y
// devuelve false. Los nombres repetidos cuentan una sola vez.
func (l *VMLocks) TryLockAll(vms ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.busy == nil {
		l.busy = make(map[string]bool)
	}
	for _, vm := range vms {
		if l.busy[vm] {
			return false
		}
	}
	for _, vm := range vms {
		l.busy[vm] = true
	}
	return true
}

// UnlockAll libera las VMs tomadas con TryLockAll.
func (l *VMLocks) UnlockAll(vms ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, vm := range vms {
		delete(l.busy, vm)
	}
}

// TryLockCreate toma el bloqueo global de creación: solo una VM o balanceador se crea a la vez
// porque todos usan el mismo disco base (001 REQ-32). Devuelve false si ya estaba tomado.
func (l *VMLocks) TryLockCreate() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.create {
		return false
	}
	l.create = true
	return true
}

// UnlockCreate libera el bloqueo global de creación.
func (l *VMLocks) UnlockCreate() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.create = false
}
