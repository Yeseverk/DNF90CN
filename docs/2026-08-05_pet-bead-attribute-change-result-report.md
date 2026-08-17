# Pet-bead attribute-change result report

## Scope

This report records the correction for the local DNF90 pet-enchant bead change ticket `490701734`. The observed input was bead `490007240` (the dark-attribute variant). No PVF, client asset, runtime configuration, or database item quantity is changed by this correction.

## Evidence

- The active `Script.pvf` resolves ticket `490701734` with 91 eligible input beads and 91 result rows, IDs `490007152` through `490007242`.
- The pool includes input `490007240` as a result with the ordinary PVF weight. Selecting that row produces the same bead template and therefore does not change the displayed attribute.
- The real-PVF regression uses the existing `op338` bridge path with `490007240`, one ticket, and account crystal-warehouse material `3037 x10`.

## Finding

The prior result selector weighted every PVF result row, including the selected input bead. It could successfully consume the ticket and material while replacing the target with the identical item ID.

## Corrected path

`limitedcube.Owner.Use` now passes the selected input item ID to the weighted selector. The selector removes matching result IDs before summing weights and rolling. A non-empty filtered pool returns one of the remaining PVF rows with its original relative weight; an empty pool returns `ErrResultSelectionFailed` before any repository mutation is committed.

## Verification

Run from `go-server`:

```text
$env:DNFBRIDGE_REAL_PVF_SMOKE='D:\DNF\runtime\data\dnf\Script.pvf'
go test -buildvcs=false -count=1 ./internal/modules/dnf/limitedcube ./internal/services/dnfbridge -run 'Test(OwnerUse|CurrentLimitedCubeOp338ChangesPetBead|RealPVFPetBeadChangeTicketConsumesCrystalWarehouseMaterial)'
```

Expected behavior: a successful use against `490007240` replaces it with a different bead item ID, consumes one ticket and ten clear cubes, and refreshes the changed inventory rows. A malformed policy with no different result fails without consuming either resource.

## Deployment

The source validation passed the full DNF module suite, complete `dnfbridge` suite, control tests, and required vet checks. The production binary at `runtime/bin/DNF90Server.exe` is SHA-256 `98DC86E60C14943F9486AC16CCA7A802CF6F516A75D7FC778FBBE71D5E8390ED`. After BAT-only restart, DNF90Server is READY (PID `7652`), portable MySQL is READY (PID `32008`), and all 120 listener probes pass.

## Prompt protocol correction

The live client prompt resolves the first `u32` after the successful `op338` status byte as an item template. The initial implementation sent ticket `490701734`, which explains the displayed ticket name despite a correctly transformed inventory bead. The limited-cube path now sends the committed `ResultItemID`; its regression asserts the response value equals the persisted target-slot bead. The separately shared crystal-contract path still sends its consumed cube ID and is unchanged.

The prompt correction was validated with the full DNF module, bridge, control, and vet suites plus the real-PVF smoke path. The deployed binary is SHA-256 `A8FC89EFA6E5E2354BDC6B24085F00CA86483FEB4D2F983E1E23ACE99E6164E5`; BAT-only restart reports DNF90Server PID `32824`, MySQL PID `34536`, and 120 accepting listeners.

## Immediate item-object replacement

The target-slot `op14` row is an in-place update in the current client. When its item template ID changes, the existing object can retain the former PVF tooltip and its local lock cache. The corrected response sequence is:

```text
op338 success (result item ID) -> op14(slot, 0xFFFFFFFF, 0) -> op14(result slot plus ticket/material rows)
```

The deletion sentinel destroys the obsolete item object, and the following repository-backed result row creates a fresh object for the new bead template. This avoids replaying a full `op13` inventory container while making the new attribute immediately inspectable and eligible for a second use.

The replacement sequence was validated through full module and bridge suites and the real-PVF smoke test. The deployed binary is SHA-256 `640B032099D180F2968AA56295F366DAD6EFE7EF6BA688C4F2DEA3DAABB73F75`; BAT-only restart reports DNF90Server PID `31556`, MySQL PID `7472`, and 120 accepting listeners.
