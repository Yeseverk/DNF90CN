# Current NoPack transport compatibility DLL

This project builds the production `90CN.dll` loaded by the existing
`ijl15.dll` plugin chain. Its production responsibility is limited to
connection bootstrap, channel-directory observation, packet codec
compatibility, DPROTO transport, bounded inbound routing, the validated
party-directory UI patch, scoped town co-presence compatibility, and two
current-client gaps that cannot be repaired by server packets alone:
allow-listed premium-contract op44 dispatch and persistent Aura state
mirroring. It also owns the optional Lua combat-power sidecar's window-thread
renderer and read-only copies of already accepted local actor stats and worn-object rows. Crystal Contract restore remains a native class-0/op898 server
notification and is not cached or applied by the DLL.

Character, inventory, equipment, avatar, creature, party, quest, contract,
mail, scene, camera, and appearance state remains owned by the Go server and
the current client's native handlers. The compatibility hooks do not grant or
persist state: they forward a native-format request or mirror an exact
server-authored Aura entitlement into the current actor's native UI state. The
optional Lua plugin can also copy a bounded text notice into the transport
DLL's main-window queue and consume confirmed client lifecycle events; it
cannot call the game UI from its worker thread.

## Production hooks

The production worker installs only the following current-EXE boundaries:

| Responsibility | Current-EXE RVA | Behavior |
| --- | --- | --- |
| TCLS direct-launch helpers | `0x015AA570`, `0x01AF7C40`, `0x01AF6970`, `0x01AF6A50`, `0x01AF6DE0`, `0x01AF6F50` | Preserves each native helper first. For the direct-launch marker only, missing native launch fields are filled from the already-parsed TCLS arguments. |
| Channel directory and connection | `0x0189FB00`, `0x014A5360`, `0x018A2440`, `0x014A2780`, `0x014A1FF0`, `0x018A0DA0` | Observes the native channel-directory lifecycle. The connection hook may redirect bootstrap port `10019` to the configured resident-channel port; it does not write HUD, clock, actor, scene, or camera state. |
| Ordinary party-directory UI | `0x02E6CF2C`, `0x02E6CF72`, `0x02E6D435`, `0x02E6D4B1`, `0x02BDD1A0` | Changes only the ordinary-channel controller's owner IDs in its guard, open, and both matching close paths from summary owner `0x17B` to full-directory owner `9`. After owner 9 opens successfully, it emits the exact native writer sequence `op98/u8(0)` found at current-EXE VA `0x032289E4`, so the server returns the full snapshot while the directory owner exists. Rows, create/join, and party state remain server-owned. |
| Town co-presence compatibility | `0x01E5CA00`, `0x02299A30`, `0x04D99C49` | Preserves native class-0 dispatch and actor lookup. It maintains only the measured remote actor context/visual lifecycle, and for the first current-character mode-0 create temporarily substitutes the native local owner when the wire owner is zero, preventing the current EXE from dereferencing a null current actor. Wire bytes are restored immediately; identity, position, equipment, party, and persistence remain server-owned. |
| Creature rename null guard | `0x0198515F` | Preserves both earlier native name updates in class-0/op101. When the final optional op105-map lookup returns null, it skips only the unsafe `sub_34DEA20(this=0x14)` copy and returns through the native epilogue; a successful lookup continues through the unchanged native copy. |
| Premium-contract right click | `0x011280C0`, `0x01772FD0`, `0x0235A060`, `0x0235A140`, `0x01F31CE0` | Preserves the native path first and emits native-format op44 only when no native op44 was produced and the selected main-bag item is in the pinned runtime-PVF premium contract allow-list. |
| Aura state mirror | `0x01E5C8A0`, `0x02BDD1A0` | Consumes only exact marked `00 AURA` / `01 AURA` op863 state notifications. It resolves the current actor through the proved actor owner at `+0x4C8` and vtable resolver `+0xA98`, then clears/sets bit 3 of the native state dword at `+0x1AC` before avatar-panel owner `0xC9` is constructed. That is the same durable gate read by current-EXE `sub_26905A0`; the DLL never calls op863 result handler `sub_1D25590`, so opening the panel cannot replay the one-shot unlock animation. Crystal Contract op898 remains native class 0 and is not handled by the DLL. |
| All-day horse-joust entrance | `0x01E645B0`, `0x00B2D126` | Retains the native owner-609 scene-state and request flow. After verifying the current EXE's exact `75 34` branch at `sub_F2D0A0+0x86`, changes only it to `EB 34`, skipping the retired local weekend/10:00 early return. The native op1291 request, state validation, and UI construction still run; opening and betting state remain server-owned. |
| Upper-body encoder | `0x02FAAFE0` | Copies the semantic plaintext body, restores packet length, and recomputes the checksum. |
| Upper-body decoder | `0x02FAB0A0` | Copies the server plaintext body into the native output contract. |
| Inbound route predicate | `0x030734E0` | Admits only the bounded set of current-client opcodes implemented by the server, then preserves the native predicate for every other opcode. |
| Outbound DPROTO boundary | `0x030738FA`, `0x0307391C` | Preserves the finalized inner-packet ownership contract while bypassing the incompatible protector wrapper for the measured routes. |
| Lua notice main-thread bridge | Existing DNF main-window procedure | `Queue90CNClientNotice` copies at most 511 UTF-16 characters into a 32-entry queue and posts a private registered window message. The DNF UI thread drains the queue and paints a click-through popup owned by the game window and this DLL. No Lua worker thread calls USER32/GDI or game UI code. Read-only analysis plus live screenshots rejected `0x01E6F9C0`: it resolves registered sound/resources and does not render arbitrary text. |
| Lua combat-power sidecar | active `0x01E5CA00` town-compatible dispatcher, private class0/op413 type 1/version 4, `0x02BD4B40`, personal owner `0xD9`, existing DNF main-window procedure | After native class0/op2 dispatch accepts a complete local mode1/mode3 body, copies the verified 92-byte base-stat fields and boundedly parses its complete worn-object create list. The Go server resolves every occupied actor slot `0..32`, including the equipped creature and the pet owner's red/blue/green artifacts, against the authoritative runtime PVF and sends an exact 70-byte private body containing affixes, identity, three attacks, and the PVF equipment-grade score. `90CN.dll` consumes that body before native dispatch and exposes ordinary-data stat, equipment and damage-affix snapshots to Lua. Malformed lengths, versions, duplicate/out-of-range slots, incomplete rows and oversized counts fail closed. `Update90CNCombatPanel` accepts Lua's bounded V6 result; a 100-ms UI-thread timer shows an owned click-through sidecar only with personal owner `0xD9` and follows its root rectangle when dragged. White/yellow/critical/yellow-additional/critical-additional/all-attack categories remain separate in the ABI and renderer. |

The DLL does not hook socket `send` or `recv`, build party records, or
grant/persist gameplay state. Its actor compatibility is limited to native
lookup/scene registration for server-created town actors, the scoped
current-character owner-zero remap described above, and mirroring the validated
class0/op205 expert-job type into the current actor field read by native menu
gates, including the type-zero state emitted after a committed expert-job
abandonment. Its only command
fallbacks are the audited op44 writer for the exact premium-contract
allow-list and the native op98 mode-0 refresh issued only after the full
party-directory owner opens.

The `90cn-decode-bypass-v1` protocol profile, supported client executable,
asset hashes, and `90CN.dll` hash form one compatibility unit. Change and
validate them together.

## Configuration

Production `90CN.dll` reads environment variables only:

| Variable | Default | Effect |
| --- | --- | --- |
| `DNF_RESIDENT_CHANNEL_ID` | `11` | Redirects bootstrap port `10019` to `10000 + channel ID`. Set `0` to disable the redirect. |
| `DNF_CIPHER_PASSTHROUGH` | enabled | Set `0` to leave the native upper-body codec and the later DPROTO/route hooks untouched. TCLS and channel bootstrap hooks are installed earlier. |
| `DNF_DPROTO_COMPAT` | enabled | Set `0` to preserve the native outbound DPROTO wrapper. |
| `DNF_ROUTE_COMPAT` | enabled | Set `0` to preserve the native inbound route predicate. |
| `DNF_PROTOCOL_TRACE` | disabled | Set `1` only during bounded transport diagnosis to write packet bodies to `90CN_protocol.log`. |
| `DNF_LUA_PLUGIN` | auto | Loads a sibling `90CNLua.dll` when present. Set `0` to disable it or `1` to log a missing plugin. The plugin and its scripts are not part of the transport DLL. |

## Optional Lua plugin

The separately built `../client-lua/90CNLua.dll` can reuse the current EXE's
statically linked Lua 5.1.5 runtime. Production `90CN.dll` only discovers and
loads that sibling module; the plugin owns its Lua hooks, script path, stack
restoration, and hot reload. Removing the module or setting
`DNF_LUA_PLUGIN=0` returns the transport DLL to its ordinary behavior.

The default external script is `lua\init.lua` beside `90CNLua.dll`.
`DNF_LUA_SCRIPT` may select an absolute path or one relative to the plugin
directory. The plugin owns a dedicated Lua thread and a plugin-owned Lua state;
timers and reloads stay on that same thread. `dnf90.notify(text)` resolves the
transport DLL's exported `Queue90CNClientNotice` interface and only submits a
copied message; the existing DNF main-window procedure paints and expires the
owned click-through popup. The transport DLL records only the proved
`ui_ready`, `ui_closed`, accepted typed class0/op24 `enter_town`, and
op28-to-first-op29-to-current-op3 `enter_dungeon` lifecycle boundaries in a
separate fixed queue. While that dungeon lifecycle is active, later complete
op29 packets publish deduplicated `dungeon_room_changed` events, and the final
accepted town op24 publishes `leave_dungeon` before `enter_town`. Later room
packets cannot replay `enter_dungeon`. The append-only event record also carries
the op28 dungeon ID and boss coordinate classification plus the fully parsed
op29 room, layer, and map context. `is_boss_room` is coordinate-backed rather
than a hard-coded map guess. The exported dequeue preserves the original
20-byte prefix for old consumers and returns the complete 60-byte record to the
current worker;
`90CNLua.dll` polls the exported `Dequeue90CNClientEvent` interface and invokes
callbacks on its Lua worker, never on the DNF UI thread. See
`../client-lua/README.md` for the build and compatibility contract.

The same optional plugin may query stat, equipment and service-authored PVF
damage-affix snapshots, calculate a custom V4 score, and submit a
`DNF90CombatPanelState`. The transport DLL renders
that copied state only on the DNF main-window thread. Its native owner-open
check is read-only; it does not create, close, move, or mutate owner `0xC9`.
The formula keeps the six damage categories visible and uses the current EXE's
complete 33-slot worn snapshot. Unsupported TGP base-stat weights, ignore-defense
and armor-mastery coefficients remain explicitly outside the score instead of
being fabricated from item IDs or unproved game-memory offsets.

Production does not read `90CN.ini` or `90CN_socket_trace.ini`. The example
files in this directory are intentionally non-operative notices and should not
be copied to the game directory as configuration.

## Diagnostics

The transport worker writes bounded lifecycle information to `90CN_trace.log`.
Full packet-boundary logging is off by default and writes
`90CN_protocol.log` only when `DNF_PROTOCOL_TRACE=1`. The vectored exception
observer records selected fault context and always returns
`EXCEPTION_CONTINUE_SEARCH`; it does not suppress or recover an exception.

These logs are diagnostic evidence only. They do not authorize client-side
gameplay fixes or make the DLL an owner of packet business semantics.

## Separate debugger test project

`90CN-debug.vcxproj` builds the independent `90CN-debug.dll` test harness.
It is not compiled into, loaded by, or required by production `90CN.dll`.
Its `DllMain` does not install hooks; a test host must explicitly call the
exported `Install90CNDebugCompat` entry.

The separate project exists only for local debugger-compatibility testing. It
must not be copied to or shipped with the game client.

Build and run its 32-bit smoke test with:

```text
msbuild 90CN-debug.vcxproj /t:Rebuild /p:Configuration=Release /p:Platform=Win32
msbuild 90CN-debug-smoke.vcxproj /t:Rebuild /p:Configuration=Release /p:Platform=Win32
Release\90CN-debug-smoke.exe Release\90CN-debug.dll
```

## Build and install

Build production `90CN.dll` as Release/Win32:

```text
msbuild 90CN.vcxproj /t:Rebuild /p:Configuration=Release /p:Platform=Win32
```

Install `Release\90CN.dll` as the game directory's `90CN.dll`. Do not deploy
`90CN-debug.dll` or either INI example.
