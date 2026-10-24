package service

import "testing"

func TestAC19_VMLocksRejectsSecondLock(t *testing.T) {
	var locks VMLocks

	steps := []struct {
		name   string
		action func() bool
		want   bool
	}{
		{"primer bloqueo de vm1", func() bool { return locks.TryLock("vm1") }, true},
		{"segundo bloqueo de vm1 se rechaza", func() bool { return locks.TryLock("vm1") }, false},
		{"otra VM no se ve afectada", func() bool { return locks.TryLock("vm2") }, true},
		{"tras liberar vm1 se puede bloquear de nuevo", func() bool { locks.Unlock("vm1"); return locks.TryLock("vm1") }, true},
	}

	for _, s := range steps {
		if got := s.action(); got != s.want {
			t.Errorf("%s: TryLock = %v, se esperaba %v", s.name, got, s.want)
		}
	}
}

// AC-34: TryLockAll toma todos los bloqueos o ninguno.
func TestAC34_TryLockAllIsAllOrNothing(t *testing.T) {
	var locks VMLocks

	if !locks.TryLock("web-a") {
		t.Fatal("TryLock(web-a) debía tener éxito")
	}
	if locks.TryLockAll("lb1", "web-a") {
		t.Error("TryLockAll(lb1, web-a) con web-a ocupada debía devolver false")
	}
	if !locks.TryLock("lb1") {
		t.Error("lb1 debía seguir libre tras un TryLockAll fallido")
	}
	locks.Unlock("lb1")

	locks.Unlock("web-a")
	if !locks.TryLockAll("lb1", "web-a") {
		t.Fatal("TryLockAll(lb1, web-a) con ambas libres debía devolver true")
	}
	if locks.TryLock("lb1") || locks.TryLock("web-a") {
		t.Error("tras TryLockAll ambas VMs debían estar ocupadas")
	}
	locks.UnlockAll("lb1", "web-a")
	if !locks.TryLockAll("lb1", "web-a") {
		t.Error("tras UnlockAll ambas VMs debían estar libres")
	}
}

// AC-34: un nombre repetido en la lista no debe bloquearse a sí mismo.
func TestAC34_TryLockAllIgnoresDuplicates(t *testing.T) {
	var locks VMLocks
	if !locks.TryLockAll("lb1", "lb1") {
		t.Fatal("TryLockAll(lb1, lb1) debía tener éxito")
	}
	locks.UnlockAll("lb1", "lb1")
	if !locks.TryLock("lb1") {
		t.Error("lb1 debía quedar libre")
	}
}

// AC-8: el bloqueo de creación es global y lo comparten create_vm y create_lb.
func TestAC8_VMLocksCreateLock(t *testing.T) {
	var locks VMLocks

	steps := []struct {
		name   string
		action func() bool
		want   bool
	}{
		{"primera creación", locks.TryLockCreate, true},
		{"segunda creación se rechaza", locks.TryLockCreate, false},
		{"tras liberar se puede crear de nuevo", func() bool { locks.UnlockCreate(); return locks.TryLockCreate() }, true},
		{"el bloqueo de creación no afecta a las VMs", func() bool { return locks.TryLock("vm1") }, true},
	}
	for _, s := range steps {
		if got := s.action(); got != s.want {
			t.Errorf("%s: = %v, se esperaba %v", s.name, got, s.want)
		}
	}
}
