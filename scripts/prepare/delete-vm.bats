#!/usr/bin/env bats
# Pruebas de delete-vm.sh con stubs de VBoxManage y sleep: no tocan VirtualBox ni ningún disco.
# El stub lleva en "$FAKE" el estado de energía de la VM; el disco conectado se elige con
# ATTACHED_UUID (diff-uuid por defecto, hijo de base-uuid; base-uuid tiene "Parent UUID: base").

load test_helper

# "run --separate-stderr" necesita bats 1.5 o superior.
bats_require_minimum_version 1.5.0

SCRIPT="$PREPARE_DIR/delete-vm.sh"

setup() {
  setup_stubs
  CALLS="$BATS_TEST_TMPDIR/calls.log"
  export FAKE="$BATS_TEST_TMPDIR/fake-vbox"
  mkdir -p "$FAKE"
  echo running >"$FAKE/vmstate"
  unset ATTACHED_UUID FAIL_CLOSEMEDIUM FAIL_UNREGISTER NO_VM
  stub_command VBoxManage "$(
    cat <<'STUB'
case "$1" in
  showvminfo)
    [[ -z "${NO_VM:-}" ]] || exit 1
    printf 'VMState="%s"\n' "$(cat "$FAKE/vmstate")"
    printf '"SATA-ImageUUID-0-0"="%s"\n' "${ATTACHED_UUID:-diff-uuid}" ;;
  showmediuminfo)
    case "${3:-}" in
      diff-uuid) printf 'UUID:           diff-uuid\nParent UUID:    base-uuid\nType:           normal (differencing)\n' ;;
      *) printf 'UUID:           base-uuid\nParent UUID:    base\nType:           multiattach\n' ;;
    esac ;;
  controlvm) [[ "${3:-}" != poweroff ]] || echo poweroff >"$FAKE/vmstate" ;;
  closemedium) [[ -z "${FAIL_CLOSEMEDIUM:-}" ]] || exit 1 ;;
  unregistervm) [[ -z "${FAIL_UNREGISTER:-}" ]] || exit 1 ;;
esac
exit 0
STUB
  )"
}

# line_of <texto fijo>: número de la primera línea de calls.log que lo contiene.
line_of() {
  grep -nF -- "$1" "$CALLS" | head -n1 | cut -d: -f1 || true
}

# assert_before <texto A> <texto B>: A aparece en calls.log antes que B.
assert_before() {
  local a b
  a="$(line_of "$1")"
  b="$(line_of "$2")"
  [ -n "$a" ] || { echo "no aparece: $1" >&2; return 1; }
  [ -n "$b" ] || { echo "no aparece: $2" >&2; return 1; }
  [ "$a" -lt "$b" ] || { echo "'$1' ($a) no va antes que '$2' ($b)" >&2; return 1; }
}

# assert_base_untouched: ninguna llamada cierra o modifica el disco base.
assert_base_untouched() {
  local touched
  touched="$(grep -E '^VBoxManage (closemedium|modifymedium) ' "$CALLS" | grep -c 'base-uuid' || true)"
  [ "$touched" -eq 0 ]
}

@test "AC-24 delete-vm.sh tiene cabecera, códigos de salida, modo estricto y secciones" {
  assert_script_structure "$SCRIPT"
}

@test "AC-31 delete-vm apaga la VM, suelta y borra el disco diferencial y la desregistra, en orden" {
  run --separate-stderr "$SCRIPT" vm1

  [ "$status" -eq 0 ]
  [ "$output" = "VM vm1 eliminada junto con su disco" ]
  assert_before "controlvm vm1 poweroff" "storageattach vm1 --storagectl SATA --port 0 --device 0 --medium none"
  assert_before "storageattach vm1 --storagectl SATA --port 0 --device 0 --medium none" "closemedium disk diff-uuid --delete"
  assert_before "closemedium disk diff-uuid --delete" "unregistervm vm1 --delete"
  assert_base_untouched
}

@test "AC-31 delete-vm no apaga una VM que ya está apagada" {
  echo poweroff >"$FAKE/vmstate"

  run "$SCRIPT" vm1

  [ "$status" -eq 0 ]
  ! grep -qF -- "controlvm" "$CALLS" || return 1
  grep -qF -- "closemedium disk diff-uuid --delete" "$CALLS"
  grep -qF -- "unregistervm vm1 --delete" "$CALLS"
}

@test "AC-31 delete-vm tolera que el disco diferencial ya no se pueda cerrar" {
  FAIL_CLOSEMEDIUM=1 run "$SCRIPT" vm1

  [ "$status" -eq 0 ]
  grep -qF -- "closemedium disk diff-uuid --delete" "$CALLS"
  grep -qF -- "unregistervm vm1 --delete" "$CALLS"
}

@test "AC-31 delete-vm termina bien si la VM ya no existe en VirtualBox" {
  NO_VM=1 run --separate-stderr "$SCRIPT" vm1

  [ "$status" -eq 0 ]
  [[ "$output" == *"no existía"* ]]
  ! grep -qF -- "unregistervm" "$CALLS" || return 1
}

@test "AC-31 delete-vm sale con 4 si no puede desregistrar la VM" {
  FAIL_UNREGISTER=1 run "$SCRIPT" vm1

  [ "$status" -eq 4 ]
}

@test "AC-38 delete-vm nunca cierra un disco sin padre (el disco base)" {
  ATTACHED_UUID="base-uuid" run "$SCRIPT" vm1

  [ "$status" -eq 0 ]
  ! grep -qF -- "closemedium" "$CALLS" || return 1
  assert_base_untouched
  grep -qF -- "unregistervm vm1 --delete" "$CALLS"
}

@test "AC-48 delete-vm rechaza argumentos inválidos sin llamar a VBoxManage ni ssh" {
  run "$SCRIPT"
  [ "$status" -eq 2 ]
  run "$SCRIPT" VM1
  [ "$status" -eq 2 ]
  run "$SCRIPT" -vm
  [ "$status" -eq 2 ]
  run "$SCRIPT" vm1 vm2
  [ "$status" -eq 2 ]
  [ ! -s "$CALLS" ]
}
