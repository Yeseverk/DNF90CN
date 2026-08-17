# Combat-power original-layout design QA

## Evidence

- Original 2017 plugin frame: `D:/DNF/client-patch/assets/combat_power_panel_skin_v2_source_original.png` (`124 x 389`).
- Exact-size V5 candidate: `D:/DNF/client-patch/assets/combat_power_panel_skin_v2_preview.png` (`124 x 389`).
- Combined comparison input: `D:/DNF/runtime/diagnostics/combat-power-tgp-reference-vs-v5-preview.png`.
- User rank/threshold reference: `C:/Users/ADMINI~1/AppData/Local/Temp/codex-clipboard-486cfb34-1e04-4b50-9e61-a58e6defdb27.png`.
- Native implementation: `D:/DNF/client-patch/90CN.cpp`, `DrawCombatPanelMain`.

## Findings

- Outer frame, title, help icon, dark-blue background, copper badge plate, gold score cell, both section-header cells, arrows, side rails, and bottom frame are the original raster rather than reconstructed shapes.
- Candidate geometry matches the source at native size. The total remains inside the gold cell, profession and level occupy the original dark identity row, base score stays inside its value cell, and the three requested equipment rows split the lower content area evenly.
- Visible equipment rows are exactly `白字`, `黄字`, and `爆伤`. Yellow-additional and critical-additional are merged into their respective displayed values; all-attack has no visible row.
- Obsolete green upgrade and bottom auto-expand controls are absent. The bottom frame remains symmetric and no replacement footer text or control was added.
- Rank artwork changes by threshold while the rank name remains inside the original copper nameplate. The preview proves no text clips the frame or overlaps a section header.

final result: blocked

Blocker: the candidate has not yet been deployed or captured in the current EXE because DNF PID `24716` is still using the preceding DLL. Offline visual comparison passes; native placement acceptance remains pending until the client exits.
