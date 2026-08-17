// 90CN / 86JP.dll - current NoPack TCLS launch compatibility plus the
// current-EXE upper-body plaintext codec bypass.
//
// This DLL keeps the six current-EXE TCLS launch helpers needed by the direct
// launcher and patches the proven sub_33AAFE0/sub_33AB0A0 upper-body codec
// boundaries plus bounded DPROTO and inbound-route transport compatibility.
// It also applies bounded current-EXE UI/state compatibility for native views
// whose managers and actor records consume separate server projections. It
// does not hook send/recv, chain-load old DLLs, replay server packet bodies, or
// own gameplay state.

#include "90CN.h"
#include "client_assets.h"

#include <windows.h>
#include <stdint.h>
#include <stdio.h>
#include <stdarg.h>
#include <string.h>
#include <time.h>
#include <wchar.h>
#include <intrin.h>

namespace
{
constexpr uintptr_t kCipherEncodeRva = 0x02FAAFE0; // sub_33AAFE0
constexpr uintptr_t kCipherDecodeRva = 0x02FAB0A0; // sub_33AB0A0
constexpr uintptr_t kCipherEncodeCallRva = 0x030569A0; // upper_pkt_flush_send13 call-site
constexpr uintptr_t kCodecSetKey0Rva = 0x03077B30;
constexpr uintptr_t kCodecSetKey2Rva = 0x03076E90;
constexpr uintptr_t kCodecSetKey3Rva = 0x03077E30;
constexpr uintptr_t kCodecSetKey7Rva = 0x03075900;
constexpr uintptr_t kCodecSetKey8Rva = 0x03078260;
constexpr uintptr_t kTclsParseRva = 0x015AA570;
constexpr uintptr_t kTclsFetchLoginRva = 0x01AF7C40;
constexpr uintptr_t kTclsFetchTextRva = 0x01AF6970;
constexpr uintptr_t kTclsFetchCryptoRva = 0x01AF6A50;
constexpr uintptr_t kTclsFetchTailRva = 0x01AF6DE0;
constexpr uintptr_t kTclsTailRva = 0x01AF6F50;
constexpr uintptr_t kGameWideAssignRva = 0x00038270;
constexpr uintptr_t kChecksumRva = 0x030545B0;
constexpr uintptr_t kDprotoRoutePredicateRva = 0x030734E0; // sub_34734E0
constexpr uintptr_t kDprotoOutgoingGateRva = 0x030738FA;   // JZ after mode-7 predicate
constexpr uintptr_t kDprotoOutgoingCloneRva = 0x0307391C;  // first mode-7 call
constexpr uintptr_t kDprotoOutgoingResumeRva = 0x03073921; // after stolen 5 bytes at clone site
constexpr uintptr_t kDprotoOutgoingReturnRva = 0x03073A2E;
constexpr uintptr_t kDprotoFalseResumeRva = 0x03073A2F;    // native predicate-false epilogue
constexpr uintptr_t kQuestAutoCompleteCallContextRva = 0x02C4DBC8; // sub_304DB90 selected quest confirmation
constexpr uintptr_t kQuestAutoCompleteCallRva = 0x02C4DBD2;
constexpr uintptr_t kQuestAutoCompleteCaptureRva = 0x02C4E12E; // sub_304E070 stores selected quest at callback+0x30
constexpr uintptr_t kQuestAutoCompleteSendRva = 0x015EE300; // sub_19EE300
constexpr uintptr_t kGameAllocatorRva = 0x00669630;
constexpr uintptr_t kGameMemcpyRva = 0x037106F0;
constexpr uintptr_t kChannelScriptDownloadRva = 0x0189FB00; // sub_1C9FB00
constexpr uintptr_t kChannelScriptLoadRva = 0x014A5360;     // sub_18A5360
constexpr uintptr_t kChannelDirectoryApplyRva = 0x018A2440; // sub_1CA2440
constexpr uintptr_t kChannelRuntimeLoadRva = 0x014A2780;     // sub_18A2780
constexpr uintptr_t kChannelCategoryInsertRva = 0x014A1FF0;  // sub_18A1FF0
constexpr uintptr_t kChannelConnectRva = 0x018A0DA0;         // sub_1CA0DA0
constexpr uintptr_t kSelectedChannelPointerRva = 0x04D88FA8; // dword_5188FA8
constexpr uintptr_t kResidentChannelRva = 0x04D89178;        // unk_5189178
constexpr uintptr_t kChannelScriptLookupRva = 0x032B1910;    // sub_36B1910
constexpr uintptr_t kChannelScriptStoreRva = 0x04D88FF0;     // unk_5188FF0
constexpr uintptr_t kPvpChannelQueryRva = 0x01498C30;        // sub_1898C30
constexpr uintptr_t kPvpQueryType8ReturnRva = 0x00DB6E81;
constexpr uintptr_t kPvpQueryType31ReturnRvaA = 0x00DB7823;
constexpr uintptr_t kPvpQueryType41ReturnRvaA = 0x00DB7840;
constexpr uintptr_t kPvpQueryType31ReturnRvaB = 0x00DB1B84;
constexpr uintptr_t kPvpQueryType41ReturnRvaB = 0x00DB1BCD;
constexpr uintptr_t kHudServerIndexDescriptorRva = 0x04DB0EE4; // dword_51B0EE4
constexpr uintptr_t kHudServerIndexValueRva = 0x04DB0EE8;      // dword_51B0EE8
constexpr uintptr_t kHudServerIndexLockRva = 0x04DB0EEC;       // dword_51B0EEC
constexpr uintptr_t kHudChannelIndexDescriptorRva = 0x04DB0EF0; // dword_51B0EF0
constexpr uintptr_t kHudChannelIndexValueRva = 0x04DB0EF4;      // dword_51B0EF4
constexpr uintptr_t kHudChannelIndexLockRva = 0x04DB0EF8;       // dword_51B0EF8
constexpr uintptr_t kServerClockDescriptorRva = 0x04DB0F20; // dword_51B0F20
constexpr uintptr_t kServerClockValueRva = 0x04DB0F24;      // dword_51B0F24
constexpr uintptr_t kLocalClockDescriptorRva = 0x04DB0F28;  // dword_51B0F28
constexpr uintptr_t kLocalClockValueRva = 0x04DB0F2C;       // dword_51B0F2C
constexpr uintptr_t kServerTickDescriptorRva = 0x04DB0F30;  // dword_51B0F30
constexpr uintptr_t kServerTickValueRva = 0x04DB0F34;       // dword_51B0F34
constexpr uintptr_t kAtomicSpinLockRva = 0x030DB470;         // sub_34DB470
constexpr uintptr_t kAtomicSpinUnlockRva = 0x030DB4A0;       // sub_34DB4A0
constexpr uintptr_t kObfuscatedStoreCase0Rva = 0x030DB910;   // sub_34DB910
constexpr uintptr_t kObfuscatedStoreCase4Rva = 0x030DB990;   // sub_34DB990
constexpr uintptr_t kObfuscatedStoreCase8Rva = 0x030DBA10;   // sub_34DBA10
constexpr uintptr_t kSelectedPageApplyRva = 0x013D6BE0;      // sub_17D6BE0
constexpr uintptr_t kSelectorCreateTickRva = 0x02FE9640;     // sub_33E9640
constexpr uintptr_t kSelectorCreateTransitionRva = 0x02FE8390; // sub_33E8390
constexpr uintptr_t kSelectorRestrictionRva = 0x04DB27D0;    // byte_51B27D0
constexpr uintptr_t kUIEventStampRva = 0x04CE0E04;           // dword_50E0E04
constexpr uintptr_t kCreateUIClickRva = 0x01AAC2A0;          // sub_1EAC2A0
constexpr uintptr_t kCreateUIOpenRva = 0x01AAC3E0;           // sub_1EAC3E0
constexpr uintptr_t kUpperCreateSendRva = 0x02FD4990;        // sub_33D4990
constexpr uintptr_t kClass0DispatchRva = 0x01E5CA00;         // sub_225CA00
constexpr uintptr_t kClass1DispatchRva = 0x01E5C8A0;         // sub_225C8A0
constexpr uintptr_t kClass0RegistryRva = 0x01E62A80;         // sub_2262A80
constexpr uintptr_t kClass1RegistryRva = 0x01E628F0;         // sub_22628F0
constexpr uintptr_t kDispatchLookupRva = 0x01E62430;         // sub_2262430
// Current NoPack event 2347 globals. These are read only after the native
// class0 handlers return, so the trace can distinguish route rejection from a
// UI/resource creation failure without mutating client-owned activity state.
constexpr uintptr_t kSpendTimeCatalogPointerRva = 0x04D78A30; // dword_5178A30
constexpr uintptr_t kSpendTimeUiSharedRva = 0x04D78A80;       // dword_5178A80
// A marked op863 state projection must not reach sub_1D25590: that result
// handler only creates the one-shot [11,8] unlock effect. Current NoPack's
// actual lock gate is sub_26905A0, which reads bit 3 from the current actor's
// state block at resolver-result+0x1AC. Mirror the marked durable state there
// before owner 0xC9 is constructed so reopening the avatar panel is silent.
constexpr uintptr_t kSceneUiOpenRva = 0x02BDD1A0;             // sub_2FDD1A0
constexpr uintptr_t kSceneUiIsOpenRva = 0x02BD4B40;           // sub_2FD4B40
// The current EXE calls sub_22645B0 from owner 609's native open gate while
// the transplanted NPC conversation is still in a blocked scene sub-state.
// That early return happens before op1291 and before the owner is promoted
// from its fallback vector. Override only that exact return address; all other
// callers retain the native scene-state decision.
constexpr uintptr_t kJoustSceneBlockCheckRva = 0x01E645B0;    // sub_22645B0
constexpr uintptr_t kJoustOpenGateReturnRva = 0x00B2D0E7;     // sub_F2D0A0+0x47
// sub_F2D0A0 checks whether the type-73 state string has a historical
// weekend/10:00 schedule before it emits the joust-opening request.  The
// local server publishes this activity as all-day; retain the native request
// and UI creation but skip only that retired client-side early return.
constexpr uintptr_t kJoustHistoricTimeGateRva = 0x00B2D126;   // jnz loc_F2D15C
constexpr uintptr_t kPartyDirectoryOwnerGuardRva = 0x02E6CF2C;
constexpr uintptr_t kPartyDirectoryOpenOwnerRva = 0x02E6CF72;
constexpr uintptr_t kPartyDirectoryCloseOwnerARva = 0x02E6D435;
constexpr uintptr_t kPartyDirectoryCloseOwnerBRva = 0x02E6D4B1;
constexpr uintptr_t kPartyDirectoryUiOwner = 9;
constexpr unsigned short kPartyDirectoryRefreshOpcode = 98;
constexpr unsigned char kPartyDirectoryRefreshModeFull = 0;
constexpr unsigned short kAuraSkinSlotOpcode = 863;
constexpr uintptr_t kAvatarPanelUiOwner = 0xC9;
constexpr uintptr_t kJoustUiOwner = 0x261;
// Current-EXE live owner-vector evidence: 0xC9 is the equipment/inventory
// panel used by the aura compatibility path, while 0xD9 owns the native
// personal-information panel opened by the character-info shortcut.
constexpr uintptr_t kPersonalInfoUiOwner = 0xD9;
constexpr size_t kSceneUiOwnerVectorOffset = 0x108;
constexpr size_t kSceneUiOwnerVectorStride = 20;
constexpr size_t kSceneUiOwnerVectorBeginOffset = 4;
constexpr size_t kSceneUiOwnerVectorEndOffset = 8;
// The personal-info object owns its root D3D widget at +0x30. The current
// widget stores the content rectangle as x/y/width/height at +0x64..+0x70.
// Its skinned frame extends 8 pixels past the content on the right and 13
// pixels above it; those are the exact sidecar attachment edges.
constexpr size_t kPersonalPanelRootWidgetOffset = 0x30;
constexpr size_t kPersonalPanelWidgetXOffset = 0x64;
constexpr size_t kPersonalPanelWidgetYOffset = 0x68;
constexpr size_t kPersonalPanelWidgetWidthOffset = 0x6C;
constexpr size_t kPersonalPanelWidgetHeightOffset = 0x70;
// The root widget excludes roughly five pixels of the visible personal-panel
// frame.  Thirteen therefore leaves the screenshot-matched six-pixel gap
// between the two visible borders instead of welding both frames together.
constexpr int kPersonalPanelFrameRight = 13;
constexpr int kPersonalPanelFrameTop = 13;
constexpr unsigned int kAuraSkinSilentRestorePacketLength = 21;
constexpr size_t kAuraSkinActorStateOwnerOffset = 0x4C8;
constexpr size_t kAuraSkinStateResolverVtableOffset = 0xA98;
constexpr size_t kAuraSkinStateFlagsOffset = 0x1AC;
constexpr unsigned int kAuraSkinUnlockedMask = 1u << 3;
constexpr uintptr_t kGameSingletonPointerRva = 0x04EEA4BC;   // dword_52EA4BC
constexpr uintptr_t kGameDispatchGateOffset = 0x002BD068;    // sub_3454600
constexpr uintptr_t kObjectManagerPointerRva = 0x04DB2728;   // dword_51B2728
constexpr uintptr_t kSceneActorManagerPointerRva = 0x04DB2738; // dword_51B2738
constexpr uintptr_t kControlledActorPointerRva = 0x04EB5BB0; // dword_52B5BB0
constexpr uintptr_t kSceneRootPointerRva = 0x04DB2764;       // dword_51B2764
constexpr uintptr_t kCurrentActorRva = 0x0229A050;           // sub_269A050
constexpr uintptr_t kActorByObjectKeyRva = 0x02299A30;       // sub_2699A30
constexpr uintptr_t kActorByContextRva = 0x013B6AA0;         // sub_17B6AA0
constexpr size_t kCurrentActorCacheOffset = 0x70;             // sub_269A050 object-manager cache
constexpr uintptr_t kResolveActorInContextRva = 0x022A2000;  // sub_26A2000
constexpr uintptr_t kActorVisualDestroyRva = 0x02294460;      // sub_2694460
constexpr uintptr_t kActorVisualCreateRva = 0x02294890;       // sub_2694890
constexpr size_t kActorVisualOffset = 0x4C4;
constexpr size_t kActorPartyOwnerOffset = 0x498;
// Current NoPack class0/op205 updates the auxiliary-profession manager, while
// the system-menu and self-click gates read the same server-owned type from the
// current actor state at actor+0x18+0x34C. The only native packet writer for
// that actor field is panel-owning op2/mode3, so mirror the validated op205
// type after its native handler succeeds instead of opening personal info.
constexpr unsigned short kExpertJobInfoOpcode = 205;
constexpr size_t kActorExpertJobTypeOffset = 0x364;
// Current live packet evidence identifies dungeon entry as one typed class-0
// op28 dungeon-info packet, the first op29 start-map packet, and then the
// exact op3 local-user activation. Later room changes emit op29 without op3.
constexpr unsigned short kDungeonUserStateOpcode = 3;
constexpr unsigned short kDungeonInfoOpcode = 28;
constexpr unsigned short kDungeonStartMapOpcode = 29;
// Current NoPack class0/op9 updates the global party slot table first, then
// refreshes HUD state 1 through sub_27310B0 only when its owner/object lookup
// resolves to the native current actor. The local bridge's established owner
// context 0 is remapped at mode-0 creation time, so a later op9 can safely
// populate the slots but miss that final pointer-equality branch. Repair only
// the op9 immediately following a successful class1/op12 SET_PARTY_INFO ACK,
// using the exact native HUD path from sub_1D64CA0.
constexpr uintptr_t kSceneHudContextRva = 0x0141E750;         // sub_181E750
constexpr uintptr_t kPartyHudRefreshRva = 0x023310B0;         // sub_27310B0
constexpr unsigned short kSetPartyInfoOpcode = 12;
constexpr unsigned short kPartyActorProjectionOpcode = 9;
constexpr unsigned short kPartyActorProjectionScene = 9999;
constexpr uintptr_t kLocalActorCreateRva = 0x01C036C0;       // sub_20036C0
constexpr uintptr_t kMode0OwnerCompareRva = 0x01C092A5;      // sub_2009160 scene/local channel cmp
constexpr uintptr_t kMode0OwnerRemoteRva = 0x01C092A9;
constexpr uintptr_t kMode0OwnerLocalRva = 0x01C092EB;
// sub_2008600 resolves the mode-3 actor with the same owner/context pair as
// mode 0.  The bridge's established context-0 sentinel must resolve to the
// current scene actor here as well; otherwise sub_2002FC0 returns null and the
// native mismatch path dereferences it at sub_24A0920.
constexpr uintptr_t kMode3OwnerResolveRva = 0x01C08737;
constexpr uintptr_t kMode3OwnerRemoteResumeRva = 0x01C08741;
constexpr uintptr_t kMode3OwnerLocalResumeRva = 0x01C0877A;
// sub_2008600 compares the same local-owner byte with the current scene
// channel a second time after resolving the actor.  Context 0 must take the
// local branch here too; otherwise sub_24A0920 marks the selected actor as a
// remote actor even though the first comparison resolved it locally.
constexpr uintptr_t kMode3OwnerFinalizeRva = 0x01C0884F;
constexpr uintptr_t kMode3OwnerFinalizeRemoteResumeRva = 0x01C0885A;
constexpr uintptr_t kMode3OwnerFinalizeLocalResumeRva = 0x01C08863;
// sub_1D84FE0's final current-creature record update assumes the equipped
// creature key is present in the op105 map. Imported/current-profile state can
// legitimately miss that lookup; the native handler otherwise dereferences
// null at sub_34DEA20(this=0x14) after it has already updated the actor and
// live creature objects.
constexpr uintptr_t kCreatureRenameMapNullCheckRva = 0x0198515F;
constexpr uintptr_t kCreatureRenameMapUpdateRva = 0x01985165;
constexpr uintptr_t kCreatureRenameMapDoneRva = 0x0198516A;
// Current NoPack sub_1D73120 parses every class0/op14 raw item row into the
// complete native dynamic-state object.  Its same-template fast path then
// updates only the count/serial and a creature trade flag.  For list 7 that
// leaves an already-created pet object holding the old enchant-card fields
// even though raw+0x0E/+0x12 and the server repository contain the replacement
// card.  The different-template branch already applies the parsed state with
// the item's native vtable +0x158 method.  Reuse that exact call on the list-7
// same-template path without replacing the item pointer.
constexpr uintptr_t kPetItemUpdateDynamicStateRva = 0x019739B9;
constexpr uintptr_t kPetItemUpdateDynamicStateResumeRva = 0x019739C0;
// sub_20695F0 scans ordinary equipment first, then the optional auxiliary
// equipment container at player+0x646C for slots 3..8. The latter is absent
// during a valid dungeon-entry window, but this one call site invokes
// sub_2326860 without the null check used by the other current-EXE callers.
// An active op903 auto-repair service reaches this window and otherwise
// dereferences 0x00000044 in sub_34DBD90.
constexpr uintptr_t kAutoRepairAuxLookupRva = 0x01C6977F;
constexpr uintptr_t kAutoRepairAuxLookupResumeRva = 0x01C69786;
constexpr uintptr_t kAutoRepairAuxLoopDoneRva = 0x01C698EA;
constexpr uintptr_t kOp24SceneModeRva = 0x01E64FD0;          // sub_2264FD0
constexpr uintptr_t kOp24LoadingGateRva = 0x022C8DA0;        // sub_26C8DA0
constexpr uintptr_t kTownEntryStateRva = 0x04D9A290;         // unk_519A290
constexpr uintptr_t kLocalOwnerServerRva = 0x04D99C4A;       // unk_5199C4A
constexpr uintptr_t kLocalOwnerChannelRva = 0x04D99C49;      // byte_5199C49
// The current actor stores its native item/equipment container at +0x646C.
// sub_249CCE0 creates this exact object with sub_759030(260) followed by
// sub_233CFD0(memory, 0, 366).  Both audited manual pickup paths refuse to call
// sub_232F810 (the native op43 writer) while this field is null.
constexpr uintptr_t kPickupContainerGetterRva = 0x020A1BC0;   // sub_24A1BC0
constexpr uintptr_t kManualPickupRva = 0x020B2840;            // sub_24B2840
constexpr uintptr_t kPickupContainerAllocatorRva = 0x00359030; // sub_759030
constexpr uintptr_t kPickupContainerConstructorRva = 0x01F3CFD0; // sub_233CFD0
constexpr size_t kPickupContainerOffset = 0x646C;
constexpr size_t kPickupContainerBytes = 260;
constexpr int kPickupContainerKind = 366;
constexpr unsigned int kDungeonTownEntryState = 3;
constexpr unsigned short kGamePortBase = 10000;
constexpr unsigned short kCrackBootstrapPort = 10019;
// Passive socket-open diagnostics.  These current-NoPack functions are never
// used to alter packets or item state: the row parser only caches exact input
// bytes, and the panel callback only logs the matching cached row.
constexpr uintptr_t kCurrentItemRowParseRva = 0x01D792A0; // sub_21792A0
constexpr uintptr_t kSocketPanelSelectRva = 0x01D12CF0;  // sub_2112CF0
constexpr uintptr_t kSocketOpenWriterRva = 0x01D12D50;  // sub_2112D50
// Current main-inventory right-click paths that serialize class1/op44.
// The fallback hooks these exact current-EXE functions and only activates for
// item ids proven by this compatibility unit's premiumlist_new.etc.
constexpr uintptr_t kInventoryUseUIARva = 0x011280C0; // sub_15280C0
constexpr uintptr_t kInventoryUseUIBRva = 0x01772FD0; // sub_1B72FD0
// These two current item-action virtual callbacks sit before the panel writer.
// sub_275A140 first asks the native 0x7D4 eligibility service and can return
// before sub_2759FD0/sub_2331CE0 is reached; sub_275A060 is its sibling route.
constexpr uintptr_t kInventoryUseGateARva = 0x0235A060; // sub_275A060
constexpr uintptr_t kInventoryUseGateBRva = 0x0235A140; // sub_275A140
// The active-coupon panel has its own native op44 writer.  Unlike the two
// generic inventory callbacks above it receives the selected template ID and
// main-bag slot directly, then applies a global UI gate before writing op44.
constexpr uintptr_t kInventoryUsePanelRva = 0x01F31CE0; // sub_2331CE0
constexpr uintptr_t kInventorySelectionManagerRva = 0x04DB2768; // dword_51B2768
constexpr uintptr_t kInventorySelectionCtorRva = 0x032EFB70; // sub_36EFB70
constexpr uintptr_t kInventorySelectionCollectRva = 0x01F31F70; // sub_2331F70
constexpr uintptr_t kInventorySelectionDtorRva = 0x030D9490; // sub_34D9490
constexpr uintptr_t kInventorySelectionLookupRva = 0x01F26860; // sub_2326860
constexpr uintptr_t kCurrentItemTemplateReadRva = 0x003CA070; // sub_7CA070
constexpr uintptr_t kCurrentItemIdentityReadRva = 0x01C64A20; // sub_2064A20
// The current main-inventory tooltip owner keeps the wrapper under the mouse
// here.  Native right-click eligibility is already true for the reproduced
// contracts, but their item class has no current-EXE op44 dispatch route.
constexpr uintptr_t kHoveredInventoryItemRva = 0x04EE1294; // dword_52E1294
constexpr uintptr_t kUpperWriterValueRva = 0x03054640; // sub_3454640
constexpr uintptr_t kUpperWriterU16Rva = 0x03054EB0; // sub_3454EB0
constexpr uintptr_t kUpperWriterU8Rva = 0x03054FC0; // sub_3454FC0
constexpr uintptr_t kUpperWriterI16Rva = 0x03054FF0; // sub_3454FF0
constexpr uintptr_t kUpperWriterU32Rva = 0x03055020; // sub_3455020
constexpr uintptr_t kUpperWriterFlushRva = 0x030566F0; // sub_34566F0
constexpr unsigned short kUseStackableOpcode = 44;
constexpr int kMainInventorySelectionGroup = 0x39;
constexpr unsigned char kMainInventoryListType = 0;
constexpr int kCurrentMainInventoryFirstItemSlot = 3;
constexpr int kCurrentMainInventoryLastItemSlot = 359;
constexpr size_t kCurrentItemIdentityOffset = 0x0E;
// sub_229ACD0 gates the two optional post-row blobs in scene op14/list3.
// It is traced only to classify the already-selected template; it does not
// alter the decision or packet reader state.
constexpr uintptr_t kSocketOp14ExtensionGateRva = 0x01E9ACD0; // sub_229ACD0
constexpr size_t kCurrentItemRawBytes = 0x77;
constexpr LONG kSocketTraceCacheRows = 2048;
constexpr LONG kSocketTraceMaxSelections = 8;
constexpr LONG kSocketTraceTemplateMatches = 8;
constexpr LONG kSocketTraceMaxWriterHits = 8;
constexpr LONG kSocketTraceMaxExtensionGateHits = 8;
// The latest confirmed current-client op914 selection is list-3 slot 9.
// Keep this exact filter narrow so the scene-reader hook is not a hot-path logger.
constexpr uint32_t kSocketTraceSelectedTemplateID = 100270515;

// Current-EXE global-minimize branches in sub_2201FE0.  Every IAT proxy checks
// the exact return address so unrelated UI and COM calls retain native behavior.
constexpr uintptr_t kToggleDesktopCoCreateReturnRva = 0x01A0227F;
constexpr uintptr_t kForeignShowWindowReturnRva = 0x01A02528;
constexpr uintptr_t kMinimizeAllSendMessageReturnRva = 0x01A02629;
// Current DNF.exe sub_384BEB0 calls CreateMutexW at RVA 0x0304BEFF,
// then compares GetLastError() with ERROR_ALREADY_EXISTS.  The return RVA
// keeps the compatibility scoped to that audited single-instance branch.
constexpr uintptr_t kMultiClientCreateMutexReturnRva = 0x0304BF05;
// Current DNF.exe sub_38AD160 calls FindWindowW at RVA 0x030AD1CA,
// focuses the existing window and returns false when it succeeds.  Returning
// no match only at this call lets the additional process create its own
// per-process registered window, matching the client's supported dual-launch
// path without changing unrelated window searches.
constexpr uintptr_t kMultiClientFindWindowReturnRva = 0x030AD1D0;

// Lua notices use an owned popup overlay instead of an unproved game UI call.
// Read-only current-EXE evidence and live screenshots rejected sub_226F9C0:
// its downstream manager hashes the string into a registered sound/resource
// table, so an arbitrary UTF-16 string is accepted without becoming visible.
// The existing DNF window procedure remains the only cross-thread boundary.
constexpr size_t kLuaNoticeMaximumCharacters = 511;
constexpr size_t kLuaNoticeQueueCapacity = 32;
constexpr size_t kLuaClientEventQueueCapacity = 32;
constexpr wchar_t kLuaNoticeWindowMessageName[] =
    L"DNF90.90CNLua.ClientNotice.90cn-decode-bypass-v1";
constexpr wchar_t kLuaNoticeOverlayClassName[] =
    L"DNF90.90CNLua.NoticeOverlay.90cn-decode-bypass-v1";
constexpr wchar_t kCombatPanelWindowMessageName[] =
    L"DNF90.90CNLua.CombatPanelUpdate.90cn-decode-bypass-v1";
constexpr wchar_t kCombatPanelOverlayClassName[] =
    L"DNF90.90CNLua.CombatPanel.90cn-decode-bypass-v1";
// The DLL-private class0/op413 envelope is kept only for the combat-power
// sidecar snapshot. Seria-luck HUD rendering must stay owned by the native EXE
// path, so type-0 lookalikes are no longer consumed here.
constexpr unsigned short kCombatPowerAffixPrivateOpcode = 413;
constexpr unsigned int kCombatPowerAffixPrivatePacketLength = 86;
constexpr unsigned char kCombatPowerAffixPrivateType = 1;
constexpr unsigned char kCombatPowerAffixPrivateVersion = 4;
constexpr UINT_PTR kLuaNoticeOverlayTimer = 0x90C1;
constexpr UINT_PTR kCombatPanelPollTimer = 0x90C2;
constexpr UINT kLuaNoticeOverlayDurationMs = 4000;
constexpr UINT kCombatPanelPollIntervalMs = 100;
constexpr int kCombatPanelWidth = 118;
constexpr int kCombatPanelHeight = 386;
constexpr int kCombatRankIconSize = 80;
constexpr int kCombatRankIconX = 19;
constexpr int kCombatRankIconY = 37;
constexpr int kCombatPanelRankTooltipWidth = 230;
constexpr int kCombatPanelRankTooltipHeight = 260;
constexpr int kCombatPanelRankHoverTop = 24;
constexpr int kCombatPanelRankHoverBottom = 158;
constexpr int kCombatPanelUpgradeButtonLeft = 26;
constexpr int kCombatPanelUpgradeButtonTop = 177;
constexpr int kCombatPanelUpgradeButtonRight = 93;
constexpr int kCombatPanelUpgradeButtonBottom = 204;
constexpr int kCombatPanelGuideWidth = 292;
constexpr int kCombatPanelGuideHeight = 238;
struct CurrentItemRowTrace {
    LONG sequence;
    uintptr_t parserObject;
    unsigned char raw[kCurrentItemRawBytes];
};

struct LuaClientNotice {
    unsigned int length;
    wchar_t text[kLuaNoticeMaximumCharacters + 1];
};

struct LuaClientEventContext {
    unsigned int flags;
    unsigned int dungeonID;
    unsigned int roomX;
    unsigned int roomY;
    unsigned int roomLayerFlag;
    unsigned int mapID;
    unsigned int previousRoomX;
    unsigned int previousRoomY;
    unsigned int previousRoomLayerFlag;
    unsigned int previousMapID;
};

enum LuaDungeonEntryStage : unsigned int {
    kLuaDungeonEntryIdle = 0,
    kLuaDungeonEntryAwaitStartMap = 1,
    kLuaDungeonEntryAwaitUserState = 2,
    kLuaDungeonEntryActive = 3,
};

uintptr_t g_dnfBase = 0;
uintptr_t g_checksumFn = 0;
uintptr_t g_gameAllocatorFn = 0;
uintptr_t g_gameMemcpyFn = 0;
uintptr_t g_dprotoOutgoingResume = 0;
uintptr_t g_dprotoOutgoingReturn = 0;
uintptr_t g_dprotoFalseResume = 0;
uintptr_t g_mode0OwnerRemoteResume = 0;
uintptr_t g_mode0OwnerLocalResume = 0;
uintptr_t g_mode3OwnerRemoteResume = 0;
uintptr_t g_mode3OwnerLocalResume = 0;
uintptr_t g_mode3OwnerFinalizeRemoteResume = 0;
uintptr_t g_mode3OwnerFinalizeLocalResume = 0;
uintptr_t g_mode3LocalOwnerChannelAddress = 0;
uintptr_t g_creatureRenameMapUpdateResume = 0;
uintptr_t g_creatureRenameMapDoneResume = 0;
uintptr_t g_petItemUpdateDynamicStateResume = 0;
uintptr_t g_autoRepairAuxLookupFn = 0;
uintptr_t g_autoRepairAuxLookupResume = 0;
uintptr_t g_autoRepairAuxLoopDone = 0;
uintptr_t g_questAutoCompleteSendFn = 0;
uintptr_t g_originalCipherEncodeFn = 0;
void* g_originalSelectedPageApply = nullptr;
void* g_originalSelectorCreateTick = nullptr;
void* g_originalSelectorCreateTransition = nullptr;
void* g_originalCreateUIClick = nullptr;
void* g_originalCreateUIOpen = nullptr;
void* g_originalUpperCreateSend = nullptr;
void* g_originalClass0Dispatch = nullptr;
void* g_originalClass1Dispatch = nullptr;
void* g_originalSceneUiOpen = nullptr;
void* g_originalJoustSceneBlockCheck = nullptr;
void* g_originalActorByObjectKey = nullptr;
void* g_originalLocalActorCreate = nullptr;
void* g_originalPickupContainerGetter = nullptr;
void* g_originalManualPickup = nullptr;
void* g_originalOp24SceneMode = nullptr;
void* g_originalOp24LoadingGate = nullptr;
void* g_originalInventoryUseUIA = nullptr;
void* g_originalInventoryUseUIB = nullptr;
void* g_originalInventoryUseGateA = nullptr;
void* g_originalInventoryUseGateB = nullptr;
void* g_originalInventoryUsePanel = nullptr;
WNDPROC g_originalDnfWindowProc = nullptr;
HWND g_dnfWindow = nullptr;
PVOID volatile g_luaNoticeBridgeWindow = nullptr;
volatile LONG g_luaNoticeWindowMessage = 0;
HWND g_luaNoticeOverlayWindow = nullptr;
wchar_t g_luaNoticeOverlayText[kLuaNoticeMaximumCharacters + 1] = {};
SRWLOCK g_luaNoticeLock = SRWLOCK_INIT;
LuaClientNotice g_luaNoticeQueue[kLuaNoticeQueueCapacity] = {};
size_t g_luaNoticeHead = 0;
size_t g_luaNoticeCount = 0;
volatile LONG g_luaNoticeQueuedCount = 0;
volatile LONG g_luaNoticeDispatchCount = 0;
volatile LONG g_luaNoticeRejectedCount = 0;
SRWLOCK g_luaClientEventLock = SRWLOCK_INIT;
DNF90ClientEvent g_luaClientEventQueue[kLuaClientEventQueueCapacity] = {};
size_t g_luaClientEventHead = 0;
size_t g_luaClientEventCount = 0;
volatile LONG g_luaClientEventSequence = 0;
volatile LONG g_luaClientEventRejectedCount = 0;
volatile LONG g_luaUiReadyEventQueued = 0;
volatile LONG g_luaUiClosedEventQueued = 0;
volatile LONG g_combatPanelWindowMessage = 0;
HWND g_combatPanelOverlayWindow = nullptr;
HWND g_combatPanelRankTooltipWindow = nullptr;
HWND g_combatPanelGuideWindow = nullptr;
bool g_combatPanelPersonalInfoOpen = false;
bool g_combatPanelRankTooltipVisible = false;
bool g_combatPanelGuideVisible = false;
SRWLOCK g_characterStatSnapshotLock = SRWLOCK_INIT;
DNF90CharacterStatSnapshot g_characterStatSnapshot = {};
volatile LONG g_characterStatGeneration = 0;
volatile LONG g_characterStatRejectLogCount = 0;
SRWLOCK g_equipmentSnapshotLock = SRWLOCK_INIT;
DNF90EquipmentSnapshot g_equipmentSnapshot = {};
volatile LONG g_equipmentSnapshotGeneration = 0;
volatile LONG g_equipmentSnapshotRejectLogCount = 0;
SRWLOCK g_damageAffixSnapshotLock = SRWLOCK_INIT;
DNF90DamageAffixSnapshot g_damageAffixSnapshot = {};
volatile LONG g_damageAffixSnapshotGeneration = 0;
volatile LONG g_damageAffixSnapshotUpdateCount = 0;
SRWLOCK g_combatPanelStateLock = SRWLOCK_INIT;
DNF90CombatPanelState g_combatPanelState = {
    sizeof(DNF90CombatPanelState), 0, 0,
    DNF90_COMBAT_PANEL_ENABLED, 1, 0, 0, 0,
    0, 0, 0, 0, 0, 0,
};
bool g_contractRButtonPending = false;
int g_contractRButtonPendingSlot = -1;
uint32_t g_contractRButtonPendingTemplateID = 0;
uint32_t g_contractRButtonPendingIdentity = 0;
LONG g_contractRButtonPendingOp44Count = 0;
volatile LONG g_cipherCallCount = 0;
volatile LONG g_clientOp44SendCount = 0;
volatile LONG g_protocolTraceEnabled = 0;
volatile LONG g_contractUseUIAHitCount = 0;
volatile LONG g_contractUseUIBHitCount = 0;
volatile LONG g_contractUseGateAHitCount = 0;
volatile LONG g_contractUseGateBHitCount = 0;
volatile LONG g_contractUsePanelHitCount = 0;
volatile LONG g_contractRButtonFallbackCount = 0;
volatile LONG g_auraSkinEntitlementState = -1;
volatile LONG g_auraSkinSilentRestoreCount = 0;
volatile LONG g_auraSkinDeferredApplyCount = 0;
volatile LONG g_partyHudRefreshPending = 0;
volatile LONG g_partyHudRefreshApplyCount = 0;
volatile LONG g_autoRepairAuxNullCount = 0;
volatile LONG g_pickupContainerRepairCount = 0;
volatile LONG g_questAutoCompleteCallCount = 0;
volatile LONG g_socketTraceRowSequence = 0;
volatile LONG g_socketTraceSelectionCount = 0;
volatile LONG g_socketTraceWriterCount = 0;
volatile LONG g_socketTraceExtensionGateCount = 0;
volatile LONG g_selectorTraceInitialized = 0;
volatile LONG g_selectorTraceMouseDown = 0;
uintptr_t g_selectorTraceSelf = 0;
uintptr_t g_selectorTraceButton = 0;
unsigned int g_selectorTraceReady = 0;
unsigned int g_selectorTracePending = 0;
unsigned int g_selectorTraceRestriction = 0;
unsigned int g_selectorTraceButtonEnabled = 0;
unsigned int g_selectorTraceButtonStamp = 0;
unsigned int g_selectorTraceGlobalStamp = 0;
int g_selectorTraceMode = -1;
int g_selectorTraceCount = -1;
void* volatile g_currentTclsParser = nullptr;
void* volatile g_originalSendMessageW = nullptr;
void* volatile g_originalShowWindow = nullptr;
void* volatile g_originalCoCreateInstance = nullptr;
void* volatile g_originalCreateMutexW = nullptr;
void* volatile g_originalFindWindowW = nullptr;
PVOID g_exceptionTraceHandle = nullptr;
thread_local bool g_traceOp24 = false;
thread_local bool g_inExceptionTrace = false;
thread_local bool g_contractUseFallbackActive = false;
thread_local bool g_pickupContainerRepairActive = false;
thread_local bool g_townActorContextLookupActive = false;
thread_local unsigned short g_townActorContextLookupKey = 0;
thread_local unsigned int g_op24SceneModeCalls = 0;
thread_local unsigned int g_op24LoadingGateCalls = 0;
thread_local bool g_op24SceneModeEarlyReturn = false;
thread_local bool g_op24LoadingGateEarlyReturn = false;
thread_local LuaDungeonEntryStage g_luaDungeonEntryStage =
    kLuaDungeonEntryIdle;
thread_local unsigned int g_luaDungeonID = 0;
thread_local unsigned char g_luaDungeonBossX = 0;
thread_local unsigned char g_luaDungeonBossY = 0;
thread_local bool g_luaDungeonRoomKnown = false;
thread_local unsigned char g_luaDungeonRoomX = 0;
thread_local unsigned char g_luaDungeonRoomY = 0;
thread_local unsigned char g_luaDungeonRoomLayerFlag = 0;
thread_local unsigned int g_luaDungeonMapID = 0;
SRWLOCK g_logWriteLock = SRWLOCK_INIT;
SRWLOCK g_socketTraceLock = SRWLOCK_INIT;
CurrentItemRowTrace g_socketTraceRows[kSocketTraceCacheRows] = {};
// Class0/op2 identifies remote town actors by an owner byte plus their stable
// uint16 object key, while op22/op23 carry the key alone. The current EXE keeps
// remotely-owned actors out of the global key table, so retain only the owner
// byte required to resolve the existing context actor for later movement and
// user interaction. The entry is cleared before native op6 leave handling.
unsigned char g_townActorOwnerByObjectKey[0x10000] = {};
unsigned char g_townActorLookupLoggedByObjectKey[0x10000] = {};
// op205 is the authoritative expert-job projection, but the current EXE's
// later local op2/mode0 actor refresh zeroes actor+0x364. Retain only the
// validated projection for the exact native actor pointer/object key so a
// different character or a remote town actor can never inherit it.
volatile LONG g_cachedExpertJobType = -1;
void* g_cachedExpertJobActor = nullptr;
unsigned short g_cachedExpertJobObjectKey = 0;
unsigned short g_currentActorObjectKey = 0;

typedef bool (__thiscall* TclsParseFn)(void*, const wchar_t*);
typedef bool (__thiscall* TclsFetchLoginFn)(void*, void*, void*, void*);
typedef bool (__thiscall* TclsFetchTextFn)(void*, int, void*);
typedef bool (__thiscall* TclsFetchOneFn)(void*, void*);
typedef bool (__stdcall* TclsTailFn)(void*);
typedef void* (__thiscall* GameWideAssignFn)(void*, const wchar_t*, size_t);
typedef int (__thiscall* CodecSetKeyFn)(void*, int, int);
typedef LRESULT (WINAPI* SendMessageWFn)(HWND, UINT, WPARAM, LPARAM);
typedef BOOL (WINAPI* ShowWindowFn)(HWND, int);
typedef HANDLE (WINAPI* CreateMutexWFn)(LPSECURITY_ATTRIBUTES, BOOL, LPCWSTR);
typedef HWND (WINAPI* FindWindowWFn)(LPCWSTR, LPCWSTR);
typedef HRESULT (WINAPI* CoCreateInstanceFn)(
    const GUID&, LPUNKNOWN, DWORD, const GUID&, LPVOID*);
typedef void (__thiscall* SelectedPageApplyFn)(void*);
typedef uintptr_t (__thiscall* SelectorCreateTickFn)(void*);
typedef uintptr_t (__thiscall* SelectorCreateTransitionFn)(void*);
typedef int (__thiscall* CreateUIClickFn)(void*);
typedef unsigned char (__thiscall* CreateUIOpenFn)(void*, int*, int);
typedef int (__thiscall* UpperCreateSendFn)(void*);
typedef int (__stdcall* Class0DispatchFn)(int, int);
typedef int (__stdcall* Class1DispatchFn)(int, int, int);
typedef void* (__thiscall* SceneUiOpenFn)(void*, uintptr_t, int, unsigned int);
typedef bool (__thiscall* SceneUiIsOpenFn)(void*, uintptr_t);
typedef bool (__thiscall* JoustSceneBlockCheckFn)(void*);
typedef void* (__thiscall* AuraSkinStateResolverFn)(void*);
typedef void* (__cdecl* DispatchRegistryFn)();
typedef void* (__thiscall* DispatchLookupFn)(void*, unsigned int);
typedef void* (__thiscall* CurrentActorFn)(void*);
typedef void* (__cdecl* ActorByObjectKeyFn)(unsigned short);
typedef void* (__cdecl* ActorByContextFn)(unsigned short, int);
typedef void* (__thiscall* ResolveActorInContextFn)(
    void*, unsigned char, unsigned short);
typedef void* (__thiscall* ActorVisualDestroyFn)(void*);
typedef int (__thiscall* ActorVisualCreateFn)(void*, int);
typedef void* (__thiscall* SceneHudContextFn)(void*);
typedef void (__thiscall* PartyHudRefreshFn)(void*, int);
typedef int (__cdecl* LocalActorCreateFn)(int);
typedef void* (__thiscall* PickupContainerGetterFn)(void*);
typedef void* (__thiscall* ManualPickupFn)(void*, void*);
typedef void* (__cdecl* PickupContainerAllocatorFn)(size_t);
typedef void* (__thiscall* PickupContainerConstructorFn)(void*, int, int);
typedef int (__thiscall* Op24SceneModeFn)(void*);
typedef unsigned char (__cdecl* Op24LoadingGateFn)();
typedef bool (__thiscall* ChannelObjectPredicateFn)(void*);
typedef int (__thiscall* ChannelObjectIntFn)(void*);
typedef unsigned short (__thiscall* ChannelObjectPortFn)(void*);
typedef const wchar_t* (__thiscall* ChannelObjectNameFn)(void*);
typedef int (__fastcall* CurrentItemRowParseFn)(int, int, char*);
typedef char (__thiscall* SocketPanelSelectFn)(void*, int, int);
typedef char (__thiscall* SocketOpenWriterFn)(void*);
typedef bool (__cdecl* SocketOp14ExtensionGateFn)(int);
typedef int (__thiscall* InventoryUseUIAFn)(void*, int, int, int, int);
typedef void (__cdecl* InventoryUseUIBFn)();
typedef uintptr_t (__thiscall* InventoryUseGateFn)(void*, int, int, int);
typedef unsigned char (__thiscall* InventoryUsePanelFn)(void*, uint32_t, int);
typedef void (__thiscall* InventorySelectionCtorFn)(void*);
typedef void (__thiscall* InventorySelectionCollectFn)(void*, void*, int);
typedef void (__thiscall* InventorySelectionDtorFn)(void*);
typedef void* (__thiscall* InventorySelectionLookupFn)(void*, int);
typedef void* (__thiscall* CurrentItemDataFn)(void*);
typedef uint32_t (__thiscall* CurrentItemTemplateReadFn)(void*);
typedef uint32_t (__cdecl* CurrentItemIdentityReadFn)(uint32_t);
typedef void* (__cdecl* UpperWriterValueFn)();
typedef void (__thiscall* UpperWriterScalarFn)(void*, uint32_t);
typedef void (__cdecl* UpperWriterFlushFn)();

TclsParseFn g_originalTclsParse = nullptr;
TclsFetchLoginFn g_originalTclsFetchLogin = nullptr;
TclsFetchTextFn g_originalTclsFetchText = nullptr;
TclsFetchOneFn g_originalTclsFetchCrypto = nullptr;
TclsFetchOneFn g_originalTclsFetchTail = nullptr;
TclsTailFn g_originalTclsTail = nullptr;
GameWideAssignFn g_gameWideAssign = nullptr;
CodecSetKeyFn g_originalCodecSetKey0 = nullptr;
SocketOp14ExtensionGateFn g_originalSocketOp14ExtensionGate = nullptr;
CodecSetKeyFn g_originalCodecSetKey2 = nullptr;
CodecSetKeyFn g_originalCodecSetKey3 = nullptr;
CodecSetKeyFn g_originalCodecSetKey7 = nullptr;
CodecSetKeyFn g_originalCodecSetKey8 = nullptr;
CurrentItemRowParseFn g_originalCurrentItemRowParse = nullptr;
SocketPanelSelectFn g_originalSocketPanelSelect = nullptr;
SocketOpenWriterFn g_originalSocketOpenWriter = nullptr;
typedef bool (__thiscall* ChannelScriptDownloadFn)(void*);
typedef bool (__thiscall* ChannelScriptLoadFn)(void*, const char*);
typedef bool (__cdecl* ChannelDirectoryApplyFn)();
typedef void* (__thiscall* ChannelRuntimeLoadFn)(void*, const void*);
typedef void* (__thiscall* ChannelScriptLookupFn)(void*, int, int);
typedef int (__stdcall* ChannelCategoryInsertFn)(int, void*);
typedef bool (__thiscall* ChannelConnectFn)(void*, int, unsigned short);
typedef unsigned int* (__stdcall* ChannelQueryFn)(unsigned int*, int);
typedef void (__thiscall* AtomicSpinFn)(void*);
typedef void (__thiscall* ObfuscatedStoreFn)(void*, const int*, int*);
ChannelScriptDownloadFn g_originalChannelScriptDownload = nullptr;
ChannelScriptLoadFn g_originalChannelScriptLoad = nullptr;
ChannelDirectoryApplyFn g_originalChannelDirectoryApply = nullptr;
ChannelRuntimeLoadFn g_originalChannelRuntimeLoad = nullptr;
ChannelCategoryInsertFn g_originalChannelCategoryInsert = nullptr;
ChannelConnectFn g_originalChannelConnect = nullptr;
ChannelQueryFn g_originalChannelQuery = nullptr;

bool BytesMatch(const unsigned char* address, const unsigned char* expected, size_t count);
void LogLine(const char* format, ...);
void QueueLuaEnterTownEventIfAccepted(
    int packet,
    unsigned int sceneModeCalls,
    unsigned int loadingGateCalls,
    bool sceneModeEarlyReturn,
    bool loadingGateEarlyReturn);
void UpdateLuaDungeonEntryStateAfterDispatch(
    int packet, unsigned int opcode);
__declspec(noinline) void LogProtocolPacket(
    const char* direction, int messageType, const void* body, int bodyLength);
__declspec(noinline) void __cdecl LogClientToServerPacket(
    int messageType, const void* body, int bodyLength);
__declspec(noinline) void __cdecl LogServerToClientPacket(
    int messageType, const void* body, int bodyLength);

__declspec(noinline) LRESULT WINAPI ProxySendMessageW(
    HWND hWnd, UINT msg, WPARAM wParam, LPARAM lParam)
{
    uintptr_t caller = reinterpret_cast<uintptr_t>(_ReturnAddress());
    if (caller == g_dnfBase + kMinimizeAllSendMessageReturnRva &&
        msg == WM_COMMAND && wParam == 0x19F && lParam == 0) {
        LogLine("[nomin] blocked audited SendMessageW branch caller_rva=0x%08X hwnd=%p",
            kMinimizeAllSendMessageReturnRva, hWnd);
        return 0;
    }

    SendMessageWFn original = reinterpret_cast<SendMessageWFn>(g_originalSendMessageW);
    if (original) return original(hWnd, msg, wParam, lParam);
    LogLine("[nomin] SendMessageW original missing caller=%p", _ReturnAddress());
    return 0;
}

__declspec(noinline) BOOL WINAPI ProxyShowWindow(HWND hWnd, int command)
{
    uintptr_t caller = reinterpret_cast<uintptr_t>(_ReturnAddress());
    if (caller == g_dnfBase + kForeignShowWindowReturnRva &&
        command == SW_MINIMIZE && hWnd) {
        DWORD ownerProcessID = 0;
        GetWindowThreadProcessId(hWnd, &ownerProcessID);
        if (ownerProcessID && ownerProcessID != GetCurrentProcessId()) {
            LogLine("[nomin] blocked audited ShowWindow branch caller_rva=0x%08X hwnd=%p pid=%lu",
                kForeignShowWindowReturnRva, hWnd, ownerProcessID);
            return TRUE;
        }
    }

    ShowWindowFn original = reinterpret_cast<ShowWindowFn>(g_originalShowWindow);
    if (original) return original(hWnd, command);
    LogLine("[nomin] ShowWindow original missing caller=%p", _ReturnAddress());
    return FALSE;
}

bool IsMultiClientLaunch()
{
    wchar_t enabled[8] = {};
    DWORD length = GetEnvironmentVariableW(
        L"DNF_MULTI_CLIENT",
        enabled,
        static_cast<DWORD>(_countof(enabled)));
    return length > 0 && length < _countof(enabled) && enabled[0] == L'1';
}

__declspec(noinline) HANDLE WINAPI ProxyCreateMutexW(
    LPSECURITY_ATTRIBUTES attributes, BOOL initialOwner, LPCWSTR name)
{
    CreateMutexWFn original =
        reinterpret_cast<CreateMutexWFn>(g_originalCreateMutexW);
    if (!original) {
        LogLine("[multi] CreateMutexW original missing caller=%p", _ReturnAddress());
        SetLastError(ERROR_PROC_NOT_FOUND);
        return nullptr;
    }

    uintptr_t caller = reinterpret_cast<uintptr_t>(_ReturnAddress());
    if (caller == g_dnfBase + kMultiClientCreateMutexReturnRva &&
        IsMultiClientLaunch() && name) {
        wchar_t uniqueName[96] = {};
        _snwprintf_s(
            uniqueName,
            _countof(uniqueName),
            _TRUNCATE,
            L"Local\\DNF90.CurrentEXE.MultiClient.%lu",
            GetCurrentProcessId());
        HANDLE result = original(attributes, initialOwner, uniqueName);
        DWORD lastError = GetLastError();
        LogLine(
            "[multi] isolated audited instance mutex caller_rva=0x%08X pid=%lu result=%p gle=%lu",
            kMultiClientCreateMutexReturnRva,
            GetCurrentProcessId(),
            result,
            lastError);
        SetLastError(lastError);
        return result;
    }

    return original(attributes, initialOwner, name);
}

__declspec(noinline) HWND WINAPI ProxyFindWindowW(
    LPCWSTR className, LPCWSTR windowName)
{
    FindWindowWFn original =
        reinterpret_cast<FindWindowWFn>(g_originalFindWindowW);
    if (!original) {
        LogLine("[multi] FindWindowW original missing caller=%p", _ReturnAddress());
        SetLastError(ERROR_PROC_NOT_FOUND);
        return nullptr;
    }

    uintptr_t caller = reinterpret_cast<uintptr_t>(_ReturnAddress());
    if (caller == g_dnfBase + kMultiClientFindWindowReturnRva &&
        IsMultiClientLaunch()) {
        LogLine(
            "[multi] isolated audited existing-window branch caller_rva=0x%08X pid=%lu",
            kMultiClientFindWindowReturnRva,
            GetCurrentProcessId());
        SetLastError(ERROR_SUCCESS);
        return nullptr;
    }
    return original(className, windowName);
}

static const GUID kShellApplicationClsid =
    {0x13709620, 0xC279, 0x11CE, {0xA4, 0x9E, 0x44, 0x45, 0x53, 0x54, 0x00, 0x00}};
static const GUID kShellDispatch4Iid =
    {0xEFD84B2D, 0x4BCF, 0x4298, {0xBE, 0x25, 0xEB, 0x54, 0x2A, 0x59, 0xFB, 0xDA}};

__declspec(noinline) HRESULT WINAPI ProxyCoCreateInstance(
    const GUID& classID, LPUNKNOWN outer, DWORD context,
    const GUID& interfaceID, LPVOID* object)
{
    uintptr_t caller = reinterpret_cast<uintptr_t>(_ReturnAddress());
    if (caller == g_dnfBase + kToggleDesktopCoCreateReturnRva &&
        IsEqualGUID(classID, kShellApplicationClsid) &&
        IsEqualGUID(interfaceID, kShellDispatch4Iid)) {
        if (object) *object = nullptr;
        LogLine("[nomin] blocked audited Shell.ToggleDesktop branch caller_rva=0x%08X",
            kToggleDesktopCoCreateReturnRva);
        return E_FAIL;
    }

    CoCreateInstanceFn original =
        reinterpret_cast<CoCreateInstanceFn>(g_originalCoCreateInstance);
    if (original) return original(classID, outer, context, interfaceID, object);
    if (object) *object = nullptr;
    LogLine("[nomin] CoCreateInstance original missing caller=%p", _ReturnAddress());
    return E_FAIL;
}

void __fastcall ProxySelectedPageApply(void* self, void* /*unused*/)
{
    if (!self) {
        LogLine("selected page apply skipped null this rva=0x%08X", kSelectedPageApplyRva);
        return;
    }
    SelectedPageApplyFn original = reinterpret_cast<SelectedPageApplyFn>(g_originalSelectedPageApply);
    if (original) original(self);
}

struct SelectorCreateTraceState {
    uintptr_t self;
    uintptr_t button;
    unsigned int ready;
    unsigned int pending;
    unsigned int restriction;
    unsigned int buttonEnabled;
    unsigned int buttonStamp;
    unsigned int globalStamp;
    int mode;
    int count;
    bool click;
};

bool ReadSelectorCreateTraceState(void* self, SelectorCreateTraceState* state)
{
    if (!state) return false;
    memset(state, 0, sizeof(*state));
    state->self = reinterpret_cast<uintptr_t>(self);
    state->mode = -1;
    state->count = -1;
    if (!self) return false;

    __try {
        unsigned char* bytes = static_cast<unsigned char*>(self);
        unsigned char* button = *reinterpret_cast<unsigned char**>(bytes + 0xD8);
        state->button = reinterpret_cast<uintptr_t>(button);
        state->ready = bytes[0x9C];
        state->pending = bytes[0xCC];
        state->mode = *reinterpret_cast<int*>(bytes + 0x8C);
        state->restriction =
            *reinterpret_cast<unsigned char*>(g_dnfBase + kSelectorRestrictionRva);
        state->globalStamp =
            *reinterpret_cast<unsigned int*>(g_dnfBase + kUIEventStampRva);

        uintptr_t begin = *reinterpret_cast<uintptr_t*>(bytes + 0xA4);
        uintptr_t end = *reinterpret_cast<uintptr_t*>(bytes + 0xA8);
        if (end >= begin && (end - begin) % sizeof(uintptr_t) == 0 &&
            (end - begin) / sizeof(uintptr_t) <= 32) {
            state->count = static_cast<int>((end - begin) / sizeof(uintptr_t));
        }

        if (button) {
            state->buttonEnabled = button[0x61];
            state->buttonStamp = *reinterpret_cast<unsigned int*>(button + 0x10C);
            state->click =
                state->buttonEnabled != 0 && state->buttonStamp == state->globalStamp;
        }
        return true;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("selector-create-state read exception self=%p code=0x%08X",
            self, GetExceptionCode());
        return false;
    }
}

uintptr_t __fastcall ProxySelectorCreateTick(void* self, void* /*unused*/)
{
    SelectorCreateTraceState before = {};
    bool stateReady = ReadSelectorCreateTraceState(self, &before);
    LONG mouseDown = (GetAsyncKeyState(VK_LBUTTON) & 0x8000) != 0 ? 1 : 0;
    LONG previousMouseDown = InterlockedExchange(&g_selectorTraceMouseDown, mouseDown);
    bool mousePressed = mouseDown != 0 && previousMouseDown == 0;
    bool changed = stateReady &&
        (InterlockedCompareExchange(&g_selectorTraceInitialized, 1, 0) == 0 ||
         before.self != g_selectorTraceSelf ||
         before.button != g_selectorTraceButton ||
         before.ready != g_selectorTraceReady ||
         before.pending != g_selectorTracePending ||
         before.restriction != g_selectorTraceRestriction ||
         before.buttonEnabled != g_selectorTraceButtonEnabled ||
         before.buttonStamp != g_selectorTraceButtonStamp ||
         before.globalStamp != g_selectorTraceGlobalStamp ||
         before.mode != g_selectorTraceMode ||
         before.count != g_selectorTraceCount);

    if (changed || before.click || mousePressed) {
        LogLine("selector-create-gate phase=before self=%p button=%p ready=%u "
            "pending=%u restriction=%u button_enabled=%u button_stamp=%u "
            "global_stamp=%u click=%u mouse_pressed=%u mode=%d count=%d",
            self, reinterpret_cast<void*>(before.button), before.ready, before.pending,
            before.restriction, before.buttonEnabled, before.buttonStamp,
            before.globalStamp, before.click ? 1u : 0u, mousePressed ? 1u : 0u,
            before.mode, before.count);
    }

    if (stateReady) {
        g_selectorTraceSelf = before.self;
        g_selectorTraceButton = before.button;
        g_selectorTraceReady = before.ready;
        g_selectorTracePending = before.pending;
        g_selectorTraceRestriction = before.restriction;
        g_selectorTraceButtonEnabled = before.buttonEnabled;
        g_selectorTraceButtonStamp = before.buttonStamp;
        g_selectorTraceGlobalStamp = before.globalStamp;
        g_selectorTraceMode = before.mode;
        g_selectorTraceCount = before.count;
    }

    SelectorCreateTickFn original =
        reinterpret_cast<SelectorCreateTickFn>(g_originalSelectorCreateTick);
    uintptr_t result = original ? original(self) : 0;

    SelectorCreateTraceState after = {};
    if ((before.click || mousePressed) &&
        ReadSelectorCreateTraceState(self, &after)) {
        LogLine("selector-create-gate phase=after self=%p ready=%u pending=%u "
            "restriction=%u mode=%d count=%d result=0x%08X",
            self, after.ready, after.pending, after.restriction, after.mode,
            after.count, static_cast<unsigned int>(result));
    }
    return result;
}

uintptr_t __fastcall ProxySelectorCreateTransition(void* self, void* /*unused*/)
{
    SelectorCreateTraceState before = {};
    ReadSelectorCreateTraceState(self, &before);
    unsigned int autoGuard = 0;
    __try {
        if (self) {
            autoGuard = static_cast<unsigned char*>(self)[0xBD4];
        }
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        autoGuard = 0xFFFFFFFFu;
    }
    LogLine("selector-create-transition phase=enter self=%p ready=%u pending=%u "
        "restriction=%u mode=%d count=%d auto_guard=%u",
        self, before.ready, before.pending, before.restriction, before.mode,
        before.count, autoGuard);

    SelectorCreateTransitionFn original =
        reinterpret_cast<SelectorCreateTransitionFn>(g_originalSelectorCreateTransition);
    uintptr_t result = original ? original(self) : 0;

    SelectorCreateTraceState after = {};
    ReadSelectorCreateTraceState(self, &after);
    LogLine("selector-create-transition phase=leave self=%p mode_before=%d "
        "mode_after=%d count=%d result=0x%08X",
        self, before.mode, after.mode, after.count, static_cast<unsigned int>(result));
    return result;
}

void ReadCreateUIState(
    void* self, void** button, int* mode, int* group,
    unsigned int* classReady, unsigned int* pending)
{
    if (button) *button = nullptr;
    if (mode) *mode = -1;
    if (group) *group = -1;
    if (classReady) *classReady = 0;
    if (pending) *pending = 0;
    if (!self) return;

    __try {
        unsigned char* bytes = static_cast<unsigned char*>(self);
        if (button) *button = *reinterpret_cast<void**>(bytes + 0x17C);
        if (group) *group = *reinterpret_cast<int*>(bytes + 0x1C4);
        if (classReady) *classReady = bytes[0x1C8];
        if (mode) *mode = *reinterpret_cast<int*>(bytes + 0x1CC);
        if (pending) *pending = bytes[0x301];
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("create-ui-state read exception self=%p code=0x%08X",
            self, GetExceptionCode());
    }
}

int __fastcall ProxyCreateUIClick(void* self, void* /*unused*/)
{
    void* button = nullptr;
    int mode = -1;
    int group = -1;
    unsigned int classReady = 0;
    unsigned int pending = 0;
    ReadCreateUIState(self, &button, &mode, &group, &classReady, &pending);
    uintptr_t caller = reinterpret_cast<uintptr_t>(_ReturnAddress());
    LogLine("create-ui-click phase=enter self=%p button=%p mode=%d group=%d "
        "class_ready=%u pending=%u caller=0x%08X caller_rva=0x%08X",
        self, button, mode, group, classReady, pending, caller,
        caller >= g_dnfBase ? caller - g_dnfBase : 0);

    CreateUIClickFn original = reinterpret_cast<CreateUIClickFn>(g_originalCreateUIClick);
    int result = original ? original(self) : 0;
    ReadCreateUIState(self, nullptr, &mode, nullptr, nullptr, &pending);
    LogLine("create-ui-click phase=leave self=%p result=0x%08X mode=%d pending=%u",
        self, result, mode, pending);
    return result;
}

unsigned char __fastcall ProxyCreateUIOpen(
    void* self, void* /*unused*/, int* requestedMode, int group)
{
    int requested = -1;
    __try {
        if (requestedMode) requested = *requestedMode;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        requested = -2;
    }
    uintptr_t caller = reinterpret_cast<uintptr_t>(_ReturnAddress());
    LogLine("create-ui-open phase=enter self=%p mode_ptr=%p requested_mode=%d "
        "group=%d caller=0x%08X caller_rva=0x%08X",
        self, requestedMode, requested, group, caller,
        caller >= g_dnfBase ? caller - g_dnfBase : 0);

    CreateUIOpenFn original = reinterpret_cast<CreateUIOpenFn>(g_originalCreateUIOpen);
    unsigned char result = original ? original(self, requestedMode, group) : 0;
    int mode = -1;
    int currentGroup = -1;
    ReadCreateUIState(self, nullptr, &mode, &currentGroup, nullptr, nullptr);
    LogLine("create-ui-open phase=leave self=%p result=%u mode=%d group=%d",
        self, static_cast<unsigned int>(result), mode, currentGroup);
    return result;
}

int __fastcall ProxyUpperCreateSend(void* self, void* /*unused*/)
{
    int mode = -1;
    int group = -1;
    unsigned int job = 0;
    unsigned int pending = 0;
    ReadCreateUIState(self, nullptr, &mode, &group, &job, &pending);
    uintptr_t caller = reinterpret_cast<uintptr_t>(_ReturnAddress());
    LogLine("upper-create-send phase=enter self=%p job=%u mode=%d group=%d "
        "pending=%u caller=0x%08X caller_rva=0x%08X",
        self, job, mode, group, pending, caller,
        caller >= g_dnfBase ? caller - g_dnfBase : 0);

    UpperCreateSendFn original =
        reinterpret_cast<UpperCreateSendFn>(g_originalUpperCreateSend);
    int result = original ? original(self) : 0;
    ReadCreateUIState(self, nullptr, nullptr, nullptr, nullptr, &pending);
    LogLine("upper-create-send phase=leave self=%p result=0x%08X pending=%u",
        self, result, pending);
    return result;
}

void LogLine(const char* format, ...)
{
    wchar_t exePath[MAX_PATH] = { 0 };
    GetModuleFileNameW(nullptr, exePath, MAX_PATH);
    wchar_t* slash = wcsrchr(exePath, L'\\');
    if (slash) slash[1] = L'\0';

    wchar_t logPath[MAX_PATH] = { 0 };
    _snwprintf(logPath, MAX_PATH - 1, L"%s90CN_trace.log", exePath);

    char line[2048] = { 0 };
    SYSTEMTIME now;
    GetLocalTime(&now);
    int offset = _snprintf(line, sizeof(line) - 2,
        "[%04d-%02d-%02d %02d:%02d:%02d.%03d] [TRACE] [pid=%u tid=%u] ",
        now.wYear, now.wMonth, now.wDay, now.wHour, now.wMinute, now.wSecond,
        now.wMilliseconds, GetCurrentProcessId(), GetCurrentThreadId());
    if (offset < 0) offset = 0;

    va_list args;
    va_start(args, format);
    int wrote = _vsnprintf(line + offset, sizeof(line) - 2 - offset, format, args);
    va_end(args);
    int length = wrote >= 0 ? offset + wrote : static_cast<int>(sizeof(line) - 2);
    line[length++] = '\n';

    AcquireSRWLockExclusive(&g_logWriteLock);
    HANDLE file = CreateFileW(logPath, FILE_APPEND_DATA, FILE_SHARE_READ | FILE_SHARE_WRITE,
        nullptr, OPEN_ALWAYS, FILE_ATTRIBUTE_NORMAL, nullptr);
    if (file != INVALID_HANDLE_VALUE) {
        DWORD ignored = 0;
        WriteFile(file, line, static_cast<DWORD>(length), &ignored, nullptr);
        CloseHandle(file);
    }
    ReleaseSRWLockExclusive(&g_logWriteLock);
}

__declspec(noinline) void LogProtocolPacket(
    const char* direction, int messageType, const void* body, int bodyLength)
{
    if (InterlockedCompareExchange(&g_protocolTraceEnabled, 0, 0) == 0) {
        return;
    }

    wchar_t exePath[MAX_PATH] = { 0 };
    GetModuleFileNameW(nullptr, exePath, MAX_PATH);
    wchar_t* slash = wcsrchr(exePath, L'\\');
    if (slash) slash[1] = L'\0';

    wchar_t logPath[MAX_PATH] = { 0 };
    _snwprintf(logPath, MAX_PATH - 1, L"%s90CN_protocol.log", exePath);

    SYSTEMTIME now;
    GetLocalTime(&now);
    char header[384] = { 0 };
    int headerLength = _snprintf(header, sizeof(header) - 1,
        "[%04d-%02d-%02d %02d:%02d:%02d.%03d] [PROTO] [packet] [pid=%u tid=%u] "
        "direction=%s msg=%d body_len=%d body=",
        now.wYear, now.wMonth, now.wDay, now.wHour, now.wMinute, now.wSecond,
        now.wMilliseconds, GetCurrentProcessId(), GetCurrentThreadId(),
        direction ? direction : "unknown", messageType, bodyLength);
    if (headerLength < 0) return;

    // Packet logging is diagnostic and must never turn an invalid native
    // pointer into a client crash.  Current create/roster packets are small;
    // cap large scene payloads while retaining their true body length.
    constexpr int kProtocolBodyLogLimit = 4096;
    int loggedLength = bodyLength;
    if (loggedLength < 0) loggedLength = 0;
    if (loggedLength > kProtocolBodyLogLimit) loggedLength = kProtocolBodyLogLimit;

    AcquireSRWLockExclusive(&g_logWriteLock);
    HANDLE file = CreateFileW(logPath, FILE_APPEND_DATA, FILE_SHARE_READ | FILE_SHARE_WRITE,
        nullptr, OPEN_ALWAYS, FILE_ATTRIBUTE_NORMAL, nullptr);
    if (file == INVALID_HANDLE_VALUE) {
        ReleaseSRWLockExclusive(&g_logWriteLock);
        return;
    }

    DWORD ignored = 0;
    WriteFile(file, header, static_cast<DWORD>(headerLength), &ignored, nullptr);

    bool readFailed = false;
    const unsigned char* bytes = static_cast<const unsigned char*>(body);
    static const char kHex[] = "0123456789ABCDEF";
    for (int base = 0; base < loggedLength && !readFailed; base += 64) {
        int count = loggedLength - base;
        if (count > 64) count = 64;
        char hex[64 * 3] = { 0 };
        int used = 0;
        __try {
            for (int i = 0; i < count; ++i) {
                unsigned char value = bytes[base + i];
                if (base != 0 || i != 0) hex[used++] = ' ';
                hex[used++] = kHex[value >> 4];
                hex[used++] = kHex[value & 0x0F];
            }
        }
        __except (EXCEPTION_EXECUTE_HANDLER) {
            readFailed = true;
        }
        if (!readFailed && used > 0) {
            WriteFile(file, hex, static_cast<DWORD>(used), &ignored, nullptr);
        }
    }

    if (readFailed) {
        static const char kReadFailure[] = " <body-read-exception>";
        WriteFile(file, kReadFailure, sizeof(kReadFailure) - 1, &ignored, nullptr);
    } else if (bodyLength > loggedLength) {
        char suffix[96] = { 0 };
        int suffixLength = _snprintf(suffix, sizeof(suffix) - 1,
            " ... <truncated logged=%d total=%d>", loggedLength, bodyLength);
        if (suffixLength > 0) {
            WriteFile(file, suffix, static_cast<DWORD>(suffixLength), &ignored, nullptr);
        }
    }
    static const char kNewline[] = "\r\n";
    WriteFile(file, kNewline, sizeof(kNewline) - 1, &ignored, nullptr);
    CloseHandle(file);
    ReleaseSRWLockExclusive(&g_logWriteLock);
}

__declspec(noinline) void __cdecl LogClientToServerPacket(
    int messageType, const void* body, int bodyLength)
{
    // The premium-contract fallback must distinguish a native op44 emitted
    // during the same right-click cycle from a missing native dispatch. Keep
    // this counter independent from optional packet-body tracing.
    if (messageType == kUseStackableOpcode) {
        InterlockedIncrement(&g_clientOp44SendCount);
    }
    LogProtocolPacket("C2S", messageType, body, bodyLength);
}

__declspec(noinline) void __cdecl LogServerToClientPacket(
    int messageType, const void* body, int bodyLength)
{
    LogProtocolPacket("S2C", messageType, body, bodyLength);
}

LONG WINAPI TraceUnhandledClientException(PEXCEPTION_POINTERS exceptionInfo)
{
    if (!exceptionInfo || !exceptionInfo->ExceptionRecord || !exceptionInfo->ContextRecord ||
        g_inExceptionTrace) {
        return EXCEPTION_CONTINUE_SEARCH;
    }

    DWORD code = exceptionInfo->ExceptionRecord->ExceptionCode;
    switch (code) {
    case EXCEPTION_ACCESS_VIOLATION:
    case EXCEPTION_ARRAY_BOUNDS_EXCEEDED:
    case EXCEPTION_ILLEGAL_INSTRUCTION:
    case EXCEPTION_INT_DIVIDE_BY_ZERO:
    case EXCEPTION_PRIV_INSTRUCTION:
    case EXCEPTION_STACK_OVERFLOW:
        break;
    default:
        return EXCEPTION_CONTINUE_SEARCH;
    }

    g_inExceptionTrace = true;
    CONTEXT* context = exceptionInfo->ContextRecord;
    ULONG_PTR accessKind = 0;
    ULONG_PTR accessAddress = 0;
    if (code == EXCEPTION_ACCESS_VIOLATION &&
        exceptionInfo->ExceptionRecord->NumberParameters >= 2) {
        accessKind = exceptionInfo->ExceptionRecord->ExceptionInformation[0];
        accessAddress = exceptionInfo->ExceptionRecord->ExceptionInformation[1];
    }
    LogLine("exception code=0x%08X address=%p access_kind=%lu access_address=0x%08lX "
        "eip=0x%08lX esp=0x%08lX ebp=0x%08lX eax=0x%08lX ebx=0x%08lX "
        "ecx=0x%08lX edx=0x%08lX esi=0x%08lX edi=0x%08lX",
        code, exceptionInfo->ExceptionRecord->ExceptionAddress,
        static_cast<unsigned long>(accessKind), static_cast<unsigned long>(accessAddress),
        context->Eip, context->Esp, context->Ebp, context->Eax, context->Ebx,
        context->Ecx, context->Edx, context->Esi, context->Edi);

    __try {
        const DWORD* stack = reinterpret_cast<const DWORD*>(context->Esp);
        LogLine("exception stack=%08lX %08lX %08lX %08lX %08lX %08lX %08lX %08lX "
            "%08lX %08lX %08lX %08lX %08lX %08lX %08lX %08lX",
            stack[0], stack[1], stack[2], stack[3], stack[4], stack[5], stack[6], stack[7],
            stack[8], stack[9], stack[10], stack[11], stack[12], stack[13], stack[14], stack[15]);
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("exception stack unreadable esp=0x%08lX", context->Esp);
    }
    g_inExceptionTrace = false;
    return EXCEPTION_CONTINUE_SEARCH;
}

void FormatHexBytes(const void* data, int length, char* output, size_t outputSize)
{
    if (!output || outputSize == 0) return;
    output[0] = '\0';
    if (!data || length <= 0) return;

    const unsigned char* bytes = static_cast<const unsigned char*>(data);
    size_t used = 0;
    int cappedLength = length > 96 ? 96 : length;
    static const char kHex[] = "0123456789ABCDEF";

    __try {
        for (int i = 0; i < cappedLength && used + 3 < outputSize; ++i) {
            unsigned char value = bytes[i];
            output[used++] = kHex[value >> 4];
            output[used++] = kHex[value & 0x0F];
            if (i + 1 < cappedLength && used + 1 < outputSize) output[used++] = ' ';
        }
        output[used] = '\0';
        if (length > cappedLength && used + 4 < outputSize) {
            output[used++] = ' ';
            output[used++] = '.';
            output[used++] = '.';
            output[used++] = '.';
            output[used] = '\0';
        }
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        _snprintf(output, outputSize - 1, "<read-exception-0x%08X>", GetExceptionCode());
        output[outputSize - 1] = '\0';
    }
}

bool IsObservedDispatchOpcode(unsigned int classID, unsigned int opcode)
{
    if (classID == 1) {
        return opcode == 4 || opcode == 7 || opcode == 10 || opcode == 11 ||
            opcode == 19 ||
            opcode == 81; // Grade-adjust ACK/result popup regression evidence.
    }
    return opcode == 1 || opcode == 2 || opcode == 5 || opcode == 9 ||
        opcode == 24 || opcode == 108 || opcode == 120 || opcode == 124 ||
        opcode == 376 || opcode == 689 || opcode == 693 || opcode == 754 ||
        opcode == 800 || opcode == 1206;
}

void TraceDispatchRegistryEntry(unsigned int classID, unsigned int opcode)
{
    void* registry = nullptr;
    void* record = nullptr;
    uintptr_t callback = 0;
    uintptr_t context = 0;
    __try {
        DispatchRegistryFn registryFn = reinterpret_cast<DispatchRegistryFn>(
            g_dnfBase + (classID == 0 ? kClass0RegistryRva : kClass1RegistryRva));
        DispatchLookupFn lookupFn = reinterpret_cast<DispatchLookupFn>(
            g_dnfBase + kDispatchLookupRva);
        registry = registryFn ? registryFn() : nullptr;
        record = registry && lookupFn ? lookupFn(registry, opcode) : nullptr;
        if (record) {
            callback = *reinterpret_cast<uintptr_t*>(record);
            context = *(reinterpret_cast<uintptr_t*>(record) + 1);
        }
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("dispatch-registry read exception class=%u opcode=%u code=0x%08X",
            classID, opcode, GetExceptionCode());
        return;
    }
    LogLine("dispatch-registry class=%u opcode=%u registry=%p record=%p callback=+0x%08X context=%p",
        classID, opcode, registry, record,
        callback ? static_cast<unsigned int>(callback - g_dnfBase) : 0,
        reinterpret_cast<void*>(context));
}

void TraceDispatchState(const char* phase, unsigned int classID, int packet)
{
    unsigned int opcode = 0;
    unsigned int bodyLead = 0;
    uintptr_t game = 0;
    int gate = -1;
    void* registry = nullptr;
    void* record = nullptr;
    uintptr_t callback = 0;
    uintptr_t context = 0;
    uintptr_t objectManager = 0;
    uintptr_t currentActor = 0;
    uintptr_t controlledActor = 0;
    uintptr_t sceneRoot = 0;
    unsigned int actorObjectKey = 0;
    uintptr_t actorByObjectKey = 0;
    int townEntryState = -1;
    unsigned int localOwnerServer = 0;
    unsigned int localOwnerChannel = 0;
    char gateBytes[128] = { 0 };

    __try {
        if (!packet) return;
        const unsigned char* bytes = reinterpret_cast<const unsigned char*>(packet);
        opcode = *reinterpret_cast<const unsigned short*>(bytes + 1);
        if (!IsObservedDispatchOpcode(classID, opcode)) return;
        bodyLead = *reinterpret_cast<const unsigned int*>(bytes + 16);

        game = *reinterpret_cast<uintptr_t*>(g_dnfBase + kGameSingletonPointerRva);
        if (game) {
            const unsigned char* gateAddress = reinterpret_cast<const unsigned char*>(
                game + kGameDispatchGateOffset);
            gate = *gateAddress;
            FormatHexBytes(gateAddress - 8, 24, gateBytes, sizeof(gateBytes));
        }

        DispatchRegistryFn registryFn = reinterpret_cast<DispatchRegistryFn>(
            g_dnfBase + (classID == 0 ? kClass0RegistryRva : kClass1RegistryRva));
        DispatchLookupFn lookupFn = reinterpret_cast<DispatchLookupFn>(
            g_dnfBase + kDispatchLookupRva);
        registry = registryFn ? registryFn() : nullptr;
        record = registry && lookupFn ? lookupFn(registry, opcode) : nullptr;
        if (record) {
            callback = *reinterpret_cast<uintptr_t*>(record);
            context = *(reinterpret_cast<uintptr_t*>(record) + 1);
        }

        objectManager = *reinterpret_cast<uintptr_t*>(g_dnfBase + kObjectManagerPointerRva);
        controlledActor = *reinterpret_cast<uintptr_t*>(g_dnfBase + kControlledActorPointerRva);
        sceneRoot = *reinterpret_cast<uintptr_t*>(g_dnfBase + kSceneRootPointerRva);
        townEntryState = *reinterpret_cast<int*>(g_dnfBase + kTownEntryStateRva);
        localOwnerServer = *reinterpret_cast<const unsigned char*>(g_dnfBase + kLocalOwnerServerRva);
        localOwnerChannel = *reinterpret_cast<const unsigned char*>(g_dnfBase + kLocalOwnerChannelRva);
        CurrentActorFn currentActorFn = reinterpret_cast<CurrentActorFn>(g_dnfBase + kCurrentActorRva);
        currentActor = objectManager && currentActorFn
            ? reinterpret_cast<uintptr_t>(currentActorFn(reinterpret_cast<void*>(objectManager)))
            : 0;

        // A mode-0 row stores its actor object key after the 0x47-byte
        // descriptor. Querying the native object table is read-only and lets
        // us distinguish a fresh create from a stale-key short circuit.
        if (classID == 0 && opcode == 2 && bytes[16] == 0 &&
            *reinterpret_cast<const unsigned short*>(bytes + 17) != 0) {
            actorObjectKey = *reinterpret_cast<const unsigned short*>(bytes + 16 + 76);
            ActorByObjectKeyFn actorByObjectKeyFn = reinterpret_cast<ActorByObjectKeyFn>(
                g_dnfBase + kActorByObjectKeyRva);
            actorByObjectKey = actorByObjectKeyFn
                ? reinterpret_cast<uintptr_t>(actorByObjectKeyFn(static_cast<unsigned short>(actorObjectKey)))
                : 0;
        }
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("dispatch-state read exception phase=%s class=%u opcode=%u code=0x%08X",
            phase, classID, opcode, GetExceptionCode());
        return;
    }

    LogLine("dispatch-state phase=%s class=%u opcode=%u body_lead=0x%08X game=%p gate=%d town_entry_state=%d local_owner=%u/%u registry=%p record=%p callback=+0x%08X context=%p object_manager=%p current_actor=%p controlled_actor=%p scene_root=%p actor_key=%u actor_by_key=%p gate_bytes=%s",
        phase, classID, opcode, bodyLead, reinterpret_cast<void*>(game), gate,
        townEntryState, localOwnerServer, localOwnerChannel, registry, record,
        callback ? static_cast<unsigned int>(callback - g_dnfBase) : 0,
        reinterpret_cast<void*>(context), reinterpret_cast<void*>(objectManager),
        reinterpret_cast<void*>(currentActor), reinterpret_cast<void*>(controlledActor),
        reinterpret_cast<void*>(sceneRoot), actorObjectKey,
        reinterpret_cast<void*>(actorByObjectKey), gateBytes);

    if (classID == 0 && (opcode == 108 || opcode == 1206)) {
        __try {
            uintptr_t catalog = *reinterpret_cast<uintptr_t*>(
                g_dnfBase + kSpendTimeCatalogPointerRva);
            uintptr_t uiObject = *reinterpret_cast<uintptr_t*>(
                g_dnfBase + kSpendTimeUiSharedRva);
            uintptr_t uiControl = *reinterpret_cast<uintptr_t*>(
                g_dnfBase + kSpendTimeUiSharedRva + sizeof(uintptr_t));
            unsigned int totalSeconds = catalog
                ? *reinterpret_cast<unsigned int*>(catalog + 5 * sizeof(uintptr_t))
                : 0;
            unsigned int rewardCount = catalog
                ? *reinterpret_cast<unsigned int*>(catalog + 6 * sizeof(uintptr_t))
                : 0;
            unsigned int completedCount = catalog
                ? *reinterpret_cast<unsigned int*>(catalog + 7 * sizeof(uintptr_t))
                : 0;
            LogLine("spend-time native phase=%s opcode=%u catalog=%p total=%u rewards=%u completed=%u ui_object=%p ui_control=%p",
                phase, opcode, reinterpret_cast<void*>(catalog), totalSeconds,
                rewardCount, completedCount, reinterpret_cast<void*>(uiObject),
                reinterpret_cast<void*>(uiControl));
        }
        __except (EXCEPTION_EXECUTE_HANDLER) {
            LogLine("spend-time native read exception phase=%s opcode=%u code=0x%08X",
                phase, opcode, GetExceptionCode());
        }
    }
}

// Read-only current-EXE evidence for town co-presence. The server can prove
// delivery, but only the native object tables can distinguish a rejected
// mode0 create from a later display-registration failure. Keep this bounded to
// the four actor-state messages and never mutate an actor or scene manager.
void TraceTownCopresenceDispatchState(const char* phase, int packet)
{
    unsigned int opcode = 0;
    unsigned int mode = 0xFF;
    unsigned int ownerContext = 0;
    unsigned int objectKey = 0;
    unsigned int recordKind = 0xFF;
    uintptr_t byObjectKey = 0;
    uintptr_t byContext = 0;
    uintptr_t sceneContext = 0;
    uintptr_t currentActor = 0;

    __try {
        if (!packet) return;
        const unsigned char* bytes = reinterpret_cast<const unsigned char*>(packet);
        opcode = *reinterpret_cast<const unsigned short*>(bytes + 1);
        unsigned int packetLength = *reinterpret_cast<const unsigned int*>(bytes + 3);
        if (packetLength < 16) return;
        const unsigned char* body = bytes + 16;
        const size_t bodyLength = packetLength - 16;

        if (opcode == 2 && bodyLength >= 5) {
            mode = body[0];
            ownerContext = body[4];
            if (mode == 0 && bodyLength >= 0x4E) {
                objectKey = *reinterpret_cast<const unsigned short*>(body + 0x4C);
            } else if (mode == 1 && bodyLength >= 0x17) {
                objectKey = *reinterpret_cast<const unsigned short*>(body + 0x15);
            } else {
                return;
            }
        } else if (opcode == 9 && bodyLength >= 10 &&
                   *reinterpret_cast<const unsigned short*>(body) != 0) {
            objectKey = *reinterpret_cast<const unsigned short*>(body + 4);
            ownerContext = body[7];
            recordKind = body[9];
        } else if (opcode == 23 && bodyLength >= 2) {
            objectKey = *reinterpret_cast<const unsigned short*>(body);
        } else if (opcode == 24 && bodyLength >= 4) {
            // Op24 has no owner context. Log its selected final row only; the
            // bridge separately records every roster row on the wire.
            unsigned int count = *reinterpret_cast<const unsigned short*>(body + 2);
            if (count != 0 && bodyLength >= 4 + count * 8) {
                objectKey = *reinterpret_cast<const unsigned short*>(
                    body + 4 + (count - 1) * 8);
            }
        } else {
            return;
        }

        uintptr_t objectManager =
            *reinterpret_cast<uintptr_t*>(g_dnfBase + kObjectManagerPointerRva);
        uintptr_t sceneActorManager =
            *reinterpret_cast<uintptr_t*>(g_dnfBase + kSceneActorManagerPointerRva);
        CurrentActorFn currentActorFn = reinterpret_cast<CurrentActorFn>(
            g_dnfBase + kCurrentActorRva);
        ActorByObjectKeyFn actorByObjectKeyFn = reinterpret_cast<ActorByObjectKeyFn>(
            g_dnfBase + kActorByObjectKeyRva);
        ActorByContextFn actorByContextFn = reinterpret_cast<ActorByContextFn>(
            g_dnfBase + kActorByContextRva);
        ResolveActorInContextFn resolveActorInContextFn =
            reinterpret_cast<ResolveActorInContextFn>(
                g_dnfBase + kResolveActorInContextRva);

        currentActor = objectManager && currentActorFn
            ? reinterpret_cast<uintptr_t>(
                currentActorFn(reinterpret_cast<void*>(objectManager)))
            : 0;
        byObjectKey = objectKey && actorByObjectKeyFn
            ? reinterpret_cast<uintptr_t>(
                actorByObjectKeyFn(static_cast<unsigned short>(objectKey)))
            : 0;
        byContext = objectKey && actorByContextFn
            ? reinterpret_cast<uintptr_t>(actorByContextFn(
                static_cast<unsigned short>(objectKey),
                static_cast<int>(ownerContext)))
            : 0;
        sceneContext = sceneActorManager && objectKey && resolveActorInContextFn
            ? reinterpret_cast<uintptr_t>(resolveActorInContextFn(
                reinterpret_cast<void*>(sceneActorManager),
                static_cast<unsigned char>(ownerContext),
                static_cast<unsigned short>(objectKey)))
            : 0;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("town-copresence-dispatch read exception phase=%s opcode=%u code=0x%08X",
            phase, opcode, GetExceptionCode());
        return;
    }

    LogLine("town-copresence-dispatch phase=%s opcode=%u mode=%u owner=%u key=%u kind=%u by_key=%p by_context=%p scene_context=%p current_actor=%p",
        phase, opcode, mode, ownerContext, objectKey, recordKind,
        reinterpret_cast<void*>(byObjectKey),
        reinterpret_cast<void*>(byContext),
        reinterpret_cast<void*>(sceneContext),
        reinterpret_cast<void*>(currentActor));
}

void* __cdecl ProxyTownActorByObjectKey(unsigned short objectKey)
{
    ActorByObjectKeyFn original = reinterpret_cast<ActorByObjectKeyFn>(
        g_originalActorByObjectKey);
    void* actor = original ? original(objectKey) : nullptr;
    if (actor || objectKey == 0) return actor;

    unsigned char ownerContext = g_townActorOwnerByObjectKey[objectKey];
    if (ownerContext == 0) return nullptr;

    ActorByContextFn actorByContextFn = reinterpret_cast<ActorByContextFn>(
        g_dnfBase + kActorByContextRva);
    actor = actorByContextFn
        ? actorByContextFn(objectKey, static_cast<int>(ownerContext))
        : nullptr;
    bool packetScoped = g_townActorContextLookupActive &&
        objectKey == g_townActorContextLookupKey;
    if (actor && !packetScoped) {
        void* visual = *reinterpret_cast<void**>(
            reinterpret_cast<uintptr_t>(actor) + kActorVisualOffset);
        if (!visual) return nullptr;
    }
    if (actor && !g_townActorLookupLoggedByObjectKey[objectKey]) {
        g_townActorLookupLoggedByObjectKey[objectKey] = 1;
        uintptr_t caller = reinterpret_cast<uintptr_t>(_ReturnAddress());
        LogLine("town co-presence actor lookup fallback key=%u owner=%u actor=%p packet_scoped=%d caller=+0x%08X",
            objectKey, ownerContext, actor, packetScoped ? 1 : 0,
            caller >= g_dnfBase ? static_cast<unsigned int>(caller - g_dnfBase) : 0);
    }
    return actor;
}

// Current NoPack's remote mode-0 branch stores the actor in the owner-context
// map but does not construct its actor+0x4C4 visual component. Live acceptance
// proved that sub_26A2980 is not a general town-presence list: adding an
// ungrouped peer to its second slot makes the native client render that peer as
// a blue-name party member. Call only the exact visual constructor reached by
// that function, leaving native party-slot membership untouched.
bool EnsureTownRemoteActorVisual(
    unsigned short objectKey, unsigned char ownerContext, void* actor)
{
    if (!objectKey || !ownerContext || !actor) return false;

    __try {
        uintptr_t objectManager =
            *reinterpret_cast<uintptr_t*>(g_dnfBase + kObjectManagerPointerRva);
        CurrentActorFn currentActorFn = reinterpret_cast<CurrentActorFn>(
            g_dnfBase + kCurrentActorRva);
        void* currentActor = objectManager && currentActorFn
            ? currentActorFn(reinterpret_cast<void*>(objectManager))
            : nullptr;
        if (actor == currentActor) return false;

        void** visual = reinterpret_cast<void**>(
            reinterpret_cast<uintptr_t>(actor) + kActorVisualOffset);
        if (*visual) {
            LogLine("town co-presence actor visual already exists key=%u owner=%u actor=%p visual=%p",
                objectKey, ownerContext, actor, *visual);
            return true;
        }

        ActorVisualCreateFn createFn = reinterpret_cast<ActorVisualCreateFn>(
            g_dnfBase + kActorVisualCreateRva);
        if (!createFn) return false;
        createFn(actor, 0);
        LogLine("town co-presence actor visual create key=%u owner=%u actor=%p visual=%p",
            objectKey, ownerContext, actor, *visual);
        return *visual != nullptr;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("town co-presence actor visual create exception key=%u owner=%u actor=%p code=0x%08X",
            objectKey, ownerContext, actor, GetExceptionCode());
        return false;
    }
}

void RemoveTownRemoteActorVisual(
    unsigned short objectKey, unsigned char ownerContext)
{
    if (!objectKey || !ownerContext) return;

    __try {
        ActorByContextFn actorByContextFn = reinterpret_cast<ActorByContextFn>(
            g_dnfBase + kActorByContextRva);
        void* actor = actorByContextFn
            ? actorByContextFn(objectKey, static_cast<int>(ownerContext))
            : nullptr;
        if (!actor) return;

        void** visual = reinterpret_cast<void**>(
            reinterpret_cast<uintptr_t>(actor) + kActorVisualOffset);
        if (!*visual) return;
        ActorVisualDestroyFn destroyFn = reinterpret_cast<ActorVisualDestroyFn>(
            g_dnfBase + kActorVisualDestroyRva);
        if (!destroyFn) return;
        void* visualBefore = *visual;
        destroyFn(actor);
        LogLine("town co-presence actor visual remove key=%u owner=%u actor=%p visual_before=%p visual_after=%p",
            objectKey, ownerContext, actor, visualBefore, *visual);
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("town co-presence actor visual remove exception key=%u owner=%u code=0x%08X",
            objectKey, ownerContext, GetExceptionCode());
    }
}

void RepairTownCurrentActor(unsigned short objectKey)
{
    __try {
        uintptr_t objectManager = *reinterpret_cast<uintptr_t*>(
            g_dnfBase + kObjectManagerPointerRva);
        if (!objectManager) {
            LogLine("town current actor repair skipped key=%u reason=no-object-manager",
                objectKey);
            return;
        }

        CurrentActorFn currentActorFn = reinterpret_cast<CurrentActorFn>(
            g_dnfBase + kCurrentActorRva);
        void* currentBefore = currentActorFn
            ? currentActorFn(reinterpret_cast<void*>(objectManager))
            : nullptr;

        ActorByObjectKeyFn originalActorByObjectKey =
            reinterpret_cast<ActorByObjectKeyFn>(g_originalActorByObjectKey);
        void* actor = originalActorByObjectKey
            ? originalActorByObjectKey(objectKey)
            : nullptr;
        const char* source = "global";
        if (!actor) {
            ActorByContextFn actorByContextFn = reinterpret_cast<ActorByContextFn>(
                g_dnfBase + kActorByContextRva);
            actor = actorByContextFn
                ? actorByContextFn(objectKey, 0)
                : nullptr;
            source = "context0";
        }

        void** cache = reinterpret_cast<void**>(
            objectManager + kCurrentActorCacheOffset);
        void* cacheBefore = *cache;
        bool repaired = false;
        if (!currentBefore && actor) {
            // sub_269A050 owns this exact cache slot and lazily fills it with
            // sub_2699A30's result. The context-0 bridge row can create the
            // selected actor after that first lazy lookup has cached null, so
            // complete the same native assignment after op2 has initialized it.
            *cache = actor;
            repaired = true;
        }

        void* currentAfter = currentActorFn
            ? currentActorFn(reinterpret_cast<void*>(objectManager))
            : *cache;
        LogLine("town current actor repair key=%u source=%s actor=%p cache_before=%p current_before=%p current_after=%p repaired=%d verified=%d",
            objectKey, source, actor, cacheBefore, currentBefore, currentAfter,
            repaired ? 1 : 0, actor && currentAfter == actor ? 1 : 0);
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("town current actor repair exception key=%u code=0x%08X",
            objectKey, GetExceptionCode());
    }
}

void SyncCurrentActorExpertJobType(unsigned int expertJobType)
{
    if (!g_dnfBase || expertJobType > 4) return;

    __try {
        void* objectManager = *reinterpret_cast<void**>(
            g_dnfBase + kObjectManagerPointerRva);
        CurrentActorFn currentActorFn = reinterpret_cast<CurrentActorFn>(
            g_dnfBase + kCurrentActorRva);
        void* actor = objectManager && currentActorFn
            ? currentActorFn(objectManager)
            : nullptr;
        if (!actor) {
            LogLine("expert-job actor sync skipped type=%u reason=current-actor-missing",
                expertJobType);
            return;
        }

        unsigned int* actorExpertJobType = reinterpret_cast<unsigned int*>(
            reinterpret_cast<uintptr_t>(actor) + kActorExpertJobTypeOffset);
        unsigned int previous = *actorExpertJobType;
        *actorExpertJobType = expertJobType;
        g_cachedExpertJobActor = actor;
        g_cachedExpertJobObjectKey = g_currentActorObjectKey;
        InterlockedExchange(&g_cachedExpertJobType, static_cast<LONG>(expertJobType));
        LogLine("expert-job actor sync type=%u previous=%u actor=%p source=class0-op205-native-manager-projection",
            expertJobType, previous, actor);
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("expert-job actor sync exception type=%u code=0x%08X",
            expertJobType, GetExceptionCode());
    }
}

void ReapplyCurrentActorExpertJobTypeAfterMode0(
    unsigned short objectKey, unsigned char ownerContext)
{
    if (!g_dnfBase || !objectKey) return;

    __try {
        void* objectManager = *reinterpret_cast<void**>(
            g_dnfBase + kObjectManagerPointerRva);
        CurrentActorFn currentActorFn = reinterpret_cast<CurrentActorFn>(
            g_dnfBase + kCurrentActorRva);
        void* currentActor = objectManager && currentActorFn
            ? currentActorFn(objectManager)
            : nullptr;
        if (!currentActor) return;

        ActorByObjectKeyFn originalActorByObjectKey =
            reinterpret_cast<ActorByObjectKeyFn>(g_originalActorByObjectKey);
        void* packetActor = originalActorByObjectKey
            ? originalActorByObjectKey(objectKey)
            : nullptr;
        if (!packetActor && ownerContext != 0) {
            ActorByContextFn actorByContextFn = reinterpret_cast<ActorByContextFn>(
                g_dnfBase + kActorByContextRva);
            packetActor = actorByContextFn
                ? actorByContextFn(objectKey, static_cast<int>(ownerContext))
                : nullptr;
        }
        if (!packetActor || packetActor != currentActor) return;

        g_currentActorObjectKey = objectKey;
        LONG cachedType = InterlockedCompareExchange(
            &g_cachedExpertJobType, -1, -1);
        if (cachedType < 0 || cachedType > 4 ||
            g_cachedExpertJobActor != currentActor ||
            (g_cachedExpertJobObjectKey != 0 &&
                g_cachedExpertJobObjectKey != objectKey)) {
            return;
        }

        unsigned int* actorExpertJobType = reinterpret_cast<unsigned int*>(
            reinterpret_cast<uintptr_t>(currentActor) +
            kActorExpertJobTypeOffset);
        unsigned int previous = *actorExpertJobType;
        *actorExpertJobType = static_cast<unsigned int>(cachedType);
        g_cachedExpertJobObjectKey = objectKey;
        LogLine("expert-job actor reproject type=%u previous=%u key=%u owner=%u actor=%p source=class0-op2-mode0-current-actor-refresh",
            static_cast<unsigned int>(cachedType), previous, objectKey,
            ownerContext, currentActor);
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("expert-job actor reproject exception key=%u owner=%u code=0x%08X",
            objectKey, ownerContext, GetExceptionCode());
    }
}

void CacheCurrentCharacterStatsFromClass0Packet(int packet);

bool TryConsumePrivateCombatPowerAffixes(int packet)
{
    DNF90DamageAffixSnapshot snapshot = {};
    bool matched = false;
    __try {
        if (packet) {
            const unsigned char* bytes =
                reinterpret_cast<const unsigned char*>(packet);
            const unsigned short opcode =
                *reinterpret_cast<const unsigned short*>(bytes + 1);
            const unsigned int packetLength =
                *reinterpret_cast<const unsigned int*>(bytes + 3);
            if (opcode == kCombatPowerAffixPrivateOpcode &&
                packetLength == kCombatPowerAffixPrivatePacketLength &&
                bytes[16] == kCombatPowerAffixPrivateType &&
                bytes[17] == kCombatPowerAffixPrivateVersion) {
                const unsigned char* body = bytes + 16;
                snapshot.size = sizeof(snapshot);
                snapshot.generation = static_cast<unsigned int>(
                    InterlockedIncrement(&g_damageAffixSnapshotGeneration));
                snapshot.validFlags =
                    DNF90_DAMAGE_AFFIX_SNAPSHOT_VALUES_VALID |
                    DNF90_DAMAGE_AFFIX_SNAPSHOT_IDENTITY_VALID |
                    DNF90_DAMAGE_AFFIX_SNAPSHOT_THREE_ATTACKS_VALID |
                    DNF90_DAMAGE_AFFIX_SNAPSHOT_EQUIPMENT_SCORE_VALID;
                snapshot.version = body[1];
                snapshot.whiteDamageTenths =
                    *reinterpret_cast<const unsigned short*>(body + 2);
                snapshot.yellowDamageTenths =
                    *reinterpret_cast<const unsigned short*>(body + 4);
                snapshot.criticalDamageTenths =
                    *reinterpret_cast<const unsigned short*>(body + 6);
                snapshot.yellowAdditionalTenths =
                    *reinterpret_cast<const unsigned short*>(body + 8);
                snapshot.criticalAdditionalTenths =
                    *reinterpret_cast<const unsigned short*>(body + 10);
                snapshot.allAttackTenths =
                    *reinterpret_cast<const unsigned short*>(body + 12);
                snapshot.equippedItemCount =
                    *reinterpret_cast<const unsigned short*>(body + 14);
                snapshot.activeSetCount =
                    *reinterpret_cast<const unsigned short*>(body + 16);
                snapshot.job = body[18];
                snapshot.growType = body[19];
                snapshot.level = body[20];
                const unsigned int professionLength = body[21];
                snapshot.physicalAttack =
                    *reinterpret_cast<const unsigned int*>(body + 22);
                snapshot.magicalAttack =
                    *reinterpret_cast<const unsigned int*>(body + 26);
                snapshot.independentAttack =
                    *reinterpret_cast<const unsigned int*>(body + 30);
                snapshot.pvfEquipmentScore =
                    *reinterpret_cast<const unsigned int*>(body + 66);
                if (professionLength > 0 &&
                    professionLength < sizeof(snapshot.professionUtf8)) {
                    memcpy(snapshot.professionUtf8, body + 34,
                        professionLength);
                    snapshot.professionUtf8[professionLength] = '\0';
                }
                matched = snapshot.equippedItemCount <=
                        DNF90_EQUIPMENT_SNAPSHOT_MAX_ITEMS &&
                    snapshot.level > 0 && professionLength > 0 &&
                    professionLength < sizeof(snapshot.professionUtf8);
            }
        }
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        return false;
    }
    if (!matched) return false;

    AcquireSRWLockExclusive(&g_damageAffixSnapshotLock);
    g_damageAffixSnapshot = snapshot;
    ReleaseSRWLockExclusive(&g_damageAffixSnapshotLock);
    LONG hit = InterlockedIncrement(&g_damageAffixSnapshotUpdateCount);
    if (hit <= 8) {
        LogLine("combat-power affix private projection consumed hit=%ld generation=%u white=%u yellow=%u critical=%u yellow_add=%u critical_add=%u all_attack=%u items=%u sets=%u job=%u grow=%u level=%u physical=%u magical=%u independent=%u pvf_equipment_score=%u profession=%s",
            hit, snapshot.generation, snapshot.whiteDamageTenths,
            snapshot.yellowDamageTenths, snapshot.criticalDamageTenths,
            snapshot.yellowAdditionalTenths,
            snapshot.criticalAdditionalTenths,
            snapshot.allAttackTenths,
            snapshot.equippedItemCount, snapshot.activeSetCount,
            snapshot.job, snapshot.growType, snapshot.level,
            snapshot.physicalAttack, snapshot.magicalAttack,
            snapshot.independentAttack, snapshot.pvfEquipmentScore,
            snapshot.professionUtf8);
    }
    return true;
}

int __stdcall ProxyTownCopresenceClass0Dispatch(int a1, int packet)
{
    if (TryConsumePrivateCombatPowerAffixes(packet)) return 1;

    TraceTownCopresenceDispatchState("before", packet);
    unsigned int opcode = 0;
    unsigned int mode = 0xFF;
    unsigned int packetLength = 0;
    unsigned int expertJobType = 0;
    bool expertJobTypePresent = false;
    unsigned short objectKey = 0;
    unsigned char ownerContext = 0;
    __try {
        if (packet) {
            const unsigned char* bytes = reinterpret_cast<const unsigned char*>(packet);
            opcode = *reinterpret_cast<const unsigned short*>(bytes + 1);
            packetLength = *reinterpret_cast<const unsigned int*>(bytes + 3);
            if (packetLength >= 16) {
                const unsigned char* body = bytes + 16;
                const size_t bodyLength = packetLength - 16;
                if ((opcode == 22 || opcode == 23 || opcode == 6) && bodyLength >= 2) {
                    objectKey = *reinterpret_cast<const unsigned short*>(body);
                } else if (opcode == 2 && bodyLength >= 5) {
                    mode = body[0];
                    ownerContext = body[4];
                    if (mode == 0 && bodyLength >= 0x4E) {
                        objectKey = *reinterpret_cast<const unsigned short*>(body + 0x4C);
                    }
                } else if (opcode == kExpertJobInfoOpcode && bodyLength >= 2 &&
                    body[1] <= 4) {
                    expertJobType = body[1];
                    expertJobTypePresent = true;
                }
            }
        }
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        opcode = 0;
        mode = 0xFF;
        expertJobType = 0;
        expertJobTypePresent = false;
        objectKey = 0;
        ownerContext = 0;
    }

    bool previousTraceOp24 = g_traceOp24;
    unsigned int previousSceneModeCalls = g_op24SceneModeCalls;
    unsigned int previousLoadingGateCalls = g_op24LoadingGateCalls;
    bool previousSceneModeEarlyReturn = g_op24SceneModeEarlyReturn;
    bool previousLoadingGateEarlyReturn = g_op24LoadingGateEarlyReturn;
    bool currentOp24 = opcode == 24;
    g_traceOp24 = currentOp24;
    if (currentOp24) {
        g_op24SceneModeCalls = 0;
        g_op24LoadingGateCalls = 0;
        g_op24SceneModeEarlyReturn = false;
        g_op24LoadingGateEarlyReturn = false;
    }

    bool previousLookupActive = g_townActorContextLookupActive;
    unsigned short previousLookupKey = g_townActorContextLookupKey;
    bool enableContextLookup =
        (opcode == 22 || opcode == 23) && objectKey != 0 &&
        g_townActorOwnerByObjectKey[objectKey] != 0;
    g_townActorContextLookupActive = enableContextLookup;
    g_townActorContextLookupKey = enableContextLookup ? objectKey : 0;

    unsigned char removeOwnerContext =
        opcode == 6 && objectKey != 0
        ? g_townActorOwnerByObjectKey[objectKey]
        : 0;
    if (removeOwnerContext != 0) {
        RemoveTownRemoteActorVisual(objectKey, removeOwnerContext);
        g_townActorOwnerByObjectKey[objectKey] = 0;
        g_townActorLookupLoggedByObjectKey[objectKey] = 0;
    }

    Class0DispatchFn original = reinterpret_cast<Class0DispatchFn>(
        g_originalClass0Dispatch);
    int result = original ? original(a1, packet) : 0;
    // This is the active current-profile class0 entrypoint. Cache the already
    // accepted local mode1/mode3 stat blob here; ProxyClass0Dispatch belongs
    // to the fallback profile and is not installed alongside this hook.
    CacheCurrentCharacterStatsFromClass0Packet(packet);
    unsigned int currentSceneModeCalls = currentOp24
        ? g_op24SceneModeCalls
        : 0;
    unsigned int currentLoadingGateCalls = currentOp24
        ? g_op24LoadingGateCalls
        : 0;
    bool currentSceneModeEarlyReturn = currentOp24 &&
        g_op24SceneModeEarlyReturn;
    bool currentLoadingGateEarlyReturn = currentOp24 &&
        g_op24LoadingGateEarlyReturn;
    g_traceOp24 = previousTraceOp24;
    g_op24SceneModeCalls = previousSceneModeCalls;
    g_op24LoadingGateCalls = previousLoadingGateCalls;
    g_op24SceneModeEarlyReturn = previousSceneModeEarlyReturn;
    g_op24LoadingGateEarlyReturn = previousLoadingGateEarlyReturn;
    g_townActorContextLookupActive = previousLookupActive;
    g_townActorContextLookupKey = previousLookupKey;

    if (expertJobTypePresent) {
        SyncCurrentActorExpertJobType(expertJobType);
    }

    if (opcode == 2 && mode == 0 && objectKey != 0 && ownerContext == 0) {
        RepairTownCurrentActor(objectKey);
    }
    if (opcode == 2 && mode == 0 && objectKey != 0) {
        ReapplyCurrentActorExpertJobTypeAfterMode0(objectKey, ownerContext);
    }
    if (opcode == 2 && mode == 0 && objectKey != 0 && ownerContext != 0) {
        ActorByContextFn actorByContextFn = reinterpret_cast<ActorByContextFn>(
            g_dnfBase + kActorByContextRva);
        void* actor = actorByContextFn
            ? actorByContextFn(objectKey, static_cast<int>(ownerContext))
            : nullptr;
        bool globalActor = false;
        if (!actor) {
            ActorByObjectKeyFn originalActorByObjectKey =
                reinterpret_cast<ActorByObjectKeyFn>(g_originalActorByObjectKey);
            actor = originalActorByObjectKey
                ? originalActorByObjectKey(objectKey)
                : nullptr;
            globalActor = actor != nullptr;
        }
        if (actor) {
            if (!globalActor) {
                g_townActorOwnerByObjectKey[objectKey] = ownerContext;
                g_townActorLookupLoggedByObjectKey[objectKey] = 0;
            }
            LogLine("town co-presence actor registered key=%u owner=%u actor=%p global=%d",
                objectKey, ownerContext, actor, globalActor ? 1 : 0);
            EnsureTownRemoteActorVisual(objectKey, ownerContext, actor);
        }
    } else if (opcode == 6 && objectKey != 0) {
        g_townActorOwnerByObjectKey[objectKey] = 0;
        if (g_currentActorObjectKey == objectKey) {
            g_currentActorObjectKey = 0;
            g_cachedExpertJobObjectKey = 0;
            g_cachedExpertJobActor = nullptr;
            InterlockedExchange(&g_cachedExpertJobType, -1);
        }
        LogLine("town co-presence actor context removed key=%u", objectKey);
    }

    TraceTownCopresenceDispatchState("after", packet);
    UpdateLuaDungeonEntryStateAfterDispatch(packet, opcode);
    if (currentOp24) {
        QueueLuaEnterTownEventIfAccepted(
            packet,
            currentSceneModeCalls,
            currentLoadingGateCalls,
            currentSceneModeEarlyReturn,
            currentLoadingGateEarlyReturn);
    }
    return result;
}

bool ReadEquipmentWireBytes(const unsigned char** cursor,
    const unsigned char* end, void* output, size_t size)
{
    if (!cursor || !*cursor || !end || *cursor > end ||
        size > static_cast<size_t>(end - *cursor)) {
        return false;
    }
    if (output && size != 0) memcpy(output, *cursor, size);
    *cursor += size;
    return true;
}

bool SkipEquipmentWireBytes(const unsigned char** cursor,
    const unsigned char* end, size_t size)
{
    return ReadEquipmentWireBytes(cursor, end, nullptr, size);
}

bool SkipEquipmentWireRawBlock(const unsigned char** cursor,
    const unsigned char* end)
{
    unsigned int length = 0;
    if (!ReadEquipmentWireBytes(cursor, end, &length, sizeof(length)) ||
        length > 1024) {
        return false;
    }
    return SkipEquipmentWireBytes(cursor, end, length);
}

bool IsCombatPowerEquipmentSlot(unsigned char actorSlot)
{
    // Current mode1/PVF evidence: 12 is weapon, 13 is title, 14..18 are
    // armor, 19..21 are accessories, 22/23 are support/magic stone, and 25
    // is earring. Slots 0..11 are avatar/aura objects; 26+ are creature,
    // medal, gems, and other independent systems.
    return (actorSlot >= 12 && actorSlot <= 23) || actorSlot == 25;
}

bool ParseCurrentEquipmentSnapshot(const unsigned char* body,
    size_t bodyLength, size_t statDataOffset,
    unsigned int sourceStatGeneration,
    DNF90EquipmentSnapshot* output)
{
    if (!body || !output) return false;

    constexpr size_t kCurrentActorStatWireBytes = 92;
    const size_t countOffset = statDataOffset +
        kCurrentActorStatWireBytes + 1; // extra-equipment-slot bitset
    if (countOffset >= bodyLength) return false;

    const unsigned char* cursor = body + countOffset;
    const unsigned char* end = body + bodyLength;
    unsigned char rowCount = 0;
    if (!ReadEquipmentWireBytes(&cursor, end, &rowCount,
            sizeof(rowCount)) ||
        rowCount > DNF90_EQUIPMENT_SNAPSHOT_MAX_ITEMS) {
        return false;
    }

    DNF90EquipmentSnapshot snapshot = {};
    snapshot.size = sizeof(snapshot);
    snapshot.sourceStatGeneration = sourceStatGeneration;
    snapshot.validFlags = DNF90_EQUIPMENT_SNAPSHOT_ROWS_VALID;
    snapshot.itemCount = rowCount;
    uint64_t seenSlots = 0;

    for (unsigned int index = 0; index < rowCount; ++index) {
        unsigned char actorSlot = 0;
        unsigned int itemID = 0;
        unsigned int qualitySeed = 0;
        unsigned char extData = 0;
        unsigned short durability = 0;
        unsigned int linkedItemID = 0;
        unsigned int auxiliaryValue = 0;
        unsigned char auxiliaryByte = 0;
        unsigned char amplifyType = 0;
        unsigned short amplifyValue = 0;
        if (!ReadEquipmentWireBytes(&cursor, end, &actorSlot,
                sizeof(actorSlot)) ||
            !ReadEquipmentWireBytes(&cursor, end, &itemID,
                sizeof(itemID)) ||
            !ReadEquipmentWireBytes(&cursor, end, &qualitySeed,
                sizeof(qualitySeed)) ||
            !ReadEquipmentWireBytes(&cursor, end, &extData,
                sizeof(extData)) ||
            !ReadEquipmentWireBytes(&cursor, end, &durability,
                sizeof(durability)) ||
            !ReadEquipmentWireBytes(&cursor, end, &linkedItemID,
                sizeof(linkedItemID)) ||
            actorSlot > 32 || itemID == 0 ||
            (seenSlots & (uint64_t(1) << actorSlot)) != 0) {
            return false;
        }
        seenSlots |= uint64_t(1) << actorSlot;

        if (linkedItemID != 0 && actorSlot == 9 &&
            !SkipEquipmentWireRawBlock(&cursor, end)) {
            return false;
        }
        if (!ReadEquipmentWireBytes(&cursor, end, &auxiliaryValue,
                sizeof(auxiliaryValue)) ||
            !ReadEquipmentWireBytes(&cursor, end, &auxiliaryByte,
                sizeof(auxiliaryByte)) ||
            !ReadEquipmentWireBytes(&cursor, end, &amplifyType,
                sizeof(amplifyType)) ||
            !ReadEquipmentWireBytes(&cursor, end, &amplifyValue,
                sizeof(amplifyValue))) {
            return false;
        }

        // Current item types 0..11 read the two avatar raw blocks; type 26
        // additionally reads a durability/instance override dword.
        if (actorSlot <= 0x0B &&
            (!SkipEquipmentWireRawBlock(&cursor, end) ||
                !SkipEquipmentWireRawBlock(&cursor, end))) {
            return false;
        }
        if (actorSlot == 26 &&
            !SkipEquipmentWireBytes(&cursor, end, sizeof(unsigned int))) {
            return false;
        }

        unsigned char vectorRecordCount = 0;
        unsigned char dwordVectorCount = 0;
        unsigned char indexedRecordCount = 0;
        if (!ReadEquipmentWireBytes(&cursor, end, &vectorRecordCount,
                sizeof(vectorRecordCount)) ||
            vectorRecordCount > 2 ||
            !SkipEquipmentWireBytes(&cursor, end,
                static_cast<size_t>(vectorRecordCount) * 8) ||
            !SkipEquipmentWireBytes(&cursor, end, sizeof(unsigned int)) ||
            !ReadEquipmentWireBytes(&cursor, end, &dwordVectorCount,
                sizeof(dwordVectorCount)) ||
            dwordVectorCount > 2 ||
            !SkipEquipmentWireBytes(&cursor, end,
                static_cast<size_t>(dwordVectorCount) * 4) ||
            !SkipEquipmentWireBytes(&cursor, end, sizeof(unsigned short)) ||
            !ReadEquipmentWireBytes(&cursor, end, &indexedRecordCount,
                sizeof(indexedRecordCount)) ||
            indexedRecordCount > 3 ||
            !SkipEquipmentWireBytes(&cursor, end,
                static_cast<size_t>(indexedRecordCount) * 3)) {
            return false;
        }
        if (indexedRecordCount != 0) {
            unsigned char activeValue = 0;
            unsigned char selectedIndex = 0;
            if (!ReadEquipmentWireBytes(&cursor, end, &activeValue,
                    sizeof(activeValue)) ||
                !ReadEquipmentWireBytes(&cursor, end, &selectedIndex,
                    sizeof(selectedIndex)) ||
                (selectedIndex != 0xFF &&
                    !SkipEquipmentWireBytes(&cursor, end, 4))) {
                return false;
            }
        }

        // state120/state160/state128/state140/state144/resource and
        // state168..170 are ten fixed bytes before the final raw blocks.
        if (!SkipEquipmentWireBytes(&cursor, end, 10) ||
            !SkipEquipmentWireRawBlock(&cursor, end) ||
            !SkipEquipmentWireRawBlock(&cursor, end) ||
            !SkipEquipmentWireBytes(&cursor, end, 1) ||
            !SkipEquipmentWireRawBlock(&cursor, end)) {
            return false;
        }

        DNF90EquippedItemSnapshot& item = snapshot.items[index];
        item.itemID = itemID;
        item.qualitySeed = qualitySeed;
        item.durability = durability;
        item.amplifyValue = amplifyValue;
        item.actorSlot = actorSlot;
        item.upgradeLevel = extData & 0x1F;
        item.amplifyType = amplifyType;
    }

    snapshot.generation = static_cast<unsigned int>(
        InterlockedIncrement(&g_equipmentSnapshotGeneration));
    *output = snapshot;
    return true;
}

void CacheCurrentCharacterStatsFromClass0Packet(int packet)
{
    if (!packet) return;

    __try {
        const unsigned char* bytes =
            reinterpret_cast<const unsigned char*>(packet);
        const unsigned int opcode =
            *reinterpret_cast<const unsigned short*>(bytes + 1);
        const unsigned int packetLength =
            *reinterpret_cast<const unsigned int*>(bytes + 3);
        if (opcode != 2 || packetLength < 16 + 0x17) return;

        const unsigned char* body = bytes + 16;
        const unsigned int bodyLength = packetLength - 16;
        const unsigned char mode = body[0];
        size_t statLengthOffset = 0;
        size_t statDataOffset = 0;
        size_t objectKeyOffset = 0;
        if (mode == 1) {
            statLengthOffset = 0x1B;
            statDataOffset = 0x1F;
            objectKeyOffset = 0x15;
        } else if (mode == 3) {
            statLengthOffset = 0x13;
            statDataOffset = 0x17;
            objectKeyOffset = 0x0D;
        } else {
            return;
        }
        constexpr unsigned int kCurrentActorStatWireBytes = 92;
        if (bodyLength < statDataOffset + kCurrentActorStatWireBytes ||
            bodyLength < statLengthOffset + sizeof(unsigned int) ||
            bodyLength < objectKeyOffset + sizeof(unsigned short) ||
            *reinterpret_cast<const unsigned int*>(
                body + statLengthOffset) != kCurrentActorStatWireBytes) {
            return;
        }

        const unsigned short objectKey =
            *reinterpret_cast<const unsigned short*>(body + objectKeyOffset);
        if (objectKey == 0) return;

        // The current local actor can be installed by the context-aware town
        // route without populating g_currentActorObjectKey. Verify the exact
        // native actor pointer instead of rejecting that accepted mode1 row.
        bool currentActorMatch = objectKey == g_currentActorObjectKey;
        void* currentActor = nullptr;
        void* packetActor = nullptr;
        void* objectManager = *reinterpret_cast<void**>(
            g_dnfBase + kObjectManagerPointerRva);
        CurrentActorFn currentActorFn = reinterpret_cast<CurrentActorFn>(
            g_dnfBase + kCurrentActorRva);
        if (objectManager && currentActorFn) {
            currentActor = currentActorFn(objectManager);
        }
        ActorByObjectKeyFn originalActorByObjectKey =
            reinterpret_cast<ActorByObjectKeyFn>(g_originalActorByObjectKey);
        if (originalActorByObjectKey) {
            packetActor = originalActorByObjectKey(objectKey);
        }
        if (!packetActor && bodyLength >= 5) {
            ActorByContextFn actorByContextFn =
                reinterpret_cast<ActorByContextFn>(
                    g_dnfBase + kActorByContextRva);
            if (actorByContextFn) {
                packetActor = actorByContextFn(
                    objectKey, static_cast<int>(body[4]));
            }
        }
        if (currentActor && packetActor == currentActor) {
            currentActorMatch = true;
            g_currentActorObjectKey = objectKey;
        }
        if (!currentActorMatch) {
            LONG rejection = InterlockedIncrement(
                &g_characterStatRejectLogCount);
            if (rejection <= 8) {
                LogLine("combat-power base snapshot ignored mode=%u key=%u "
                    "cached_key=%u current_actor=%p packet_actor=%p",
                    static_cast<unsigned int>(mode), objectKey,
                    static_cast<unsigned int>(g_currentActorObjectKey),
                    currentActor, packetActor);
            }
            return;
        }

        const unsigned char* stat = body + statDataOffset;
        DNF90CharacterStatSnapshot snapshot = {};
        snapshot.size = sizeof(snapshot);
        snapshot.generation = static_cast<unsigned int>(
            InterlockedIncrement(&g_characterStatGeneration));
        snapshot.validFlags = DNF90_CHARACTER_STATS_BASE_VALID |
            DNF90_CHARACTER_STATS_SPEED_VALID;
        snapshot.hpMax = *reinterpret_cast<const unsigned int*>(stat + 0);
        snapshot.mpMax = *reinterpret_cast<const unsigned int*>(stat + 4);
        snapshot.strength =
            *reinterpret_cast<const short*>(stat + 8) / 10;
        snapshot.vitality =
            *reinterpret_cast<const short*>(stat + 10) / 10;
        snapshot.intelligence =
            *reinterpret_cast<const short*>(stat + 12) / 10;
        snapshot.spirit =
            *reinterpret_cast<const short*>(stat + 14) / 10;
        snapshot.moveSpeed =
            *reinterpret_cast<const unsigned int*>(stat + 68);
        snapshot.attackSpeed =
            *reinterpret_cast<const unsigned short*>(stat + 72);
        snapshot.castSpeed =
            *reinterpret_cast<const unsigned short*>(stat + 74);

        AcquireSRWLockExclusive(&g_characterStatSnapshotLock);
        g_characterStatSnapshot = snapshot;
        ReleaseSRWLockExclusive(&g_characterStatSnapshotLock);
        LogLine("combat-power base snapshot generation=%u mode=%u key=%u "
            "hp=%u mp=%u str=%d vit=%d int=%d spi=%d",
            snapshot.generation, static_cast<unsigned int>(mode), objectKey,
            snapshot.hpMax, snapshot.mpMax, snapshot.strength,
            snapshot.vitality, snapshot.intelligence, snapshot.spirit);

        DNF90EquipmentSnapshot equipment = {};
        if (!ParseCurrentEquipmentSnapshot(body, bodyLength, statDataOffset,
                snapshot.generation, &equipment)) {
            LONG rejection = InterlockedIncrement(
                &g_equipmentSnapshotRejectLogCount);
            if (rejection <= 8) {
                LogLine("combat-power equipment snapshot ignored mode=%u "
                    "key=%u body_len=%u stat_generation=%u",
                    static_cast<unsigned int>(mode), objectKey, bodyLength,
                    snapshot.generation);
            }
            return;
        }

        unsigned int combatItems = 0;
        unsigned int upgradedItems = 0;
        unsigned int amplifiedItems = 0;
        for (unsigned int index = 0; index < equipment.itemCount; ++index) {
            const DNF90EquippedItemSnapshot& item = equipment.items[index];
            if (!IsCombatPowerEquipmentSlot(item.actorSlot)) continue;
            ++combatItems;
            if (item.upgradeLevel != 0) ++upgradedItems;
            if (item.amplifyType >= 1 && item.amplifyType <= 4 &&
                item.amplifyValue != 0) {
                ++amplifiedItems;
            }
        }
        AcquireSRWLockExclusive(&g_equipmentSnapshotLock);
        g_equipmentSnapshot = equipment;
        ReleaseSRWLockExclusive(&g_equipmentSnapshotLock);
        LogLine("combat-power equipment snapshot generation=%u "
            "stat_generation=%u mode=%u key=%u rows=%u combat_items=%u "
            "upgraded_items=%u amplified_items=%u",
            equipment.generation, equipment.sourceStatGeneration,
            static_cast<unsigned int>(mode), objectKey,
            equipment.itemCount, combatItems, upgradedItems, amplifiedItems);
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("combat-power base snapshot exception code=0x%08X",
            GetExceptionCode());
    }
}

int __stdcall ProxyClass0Dispatch(int a1, int packet)
{
    TraceDispatchState("before", 0, packet);
    bool previousTraceOp24 = g_traceOp24;
    unsigned int previousSceneModeCalls = g_op24SceneModeCalls;
    unsigned int previousLoadingGateCalls = g_op24LoadingGateCalls;
    __try {
        g_traceOp24 = packet && *reinterpret_cast<const unsigned short*>(packet + 1) == 24;
        if (g_traceOp24) {
            g_op24SceneModeCalls = 0;
            g_op24LoadingGateCalls = 0;
        }
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        g_traceOp24 = false;
    }
    Class0DispatchFn original = reinterpret_cast<Class0DispatchFn>(g_originalClass0Dispatch);
    int result = original ? original(a1, packet) : 0;
    g_traceOp24 = previousTraceOp24;
    g_op24SceneModeCalls = previousSceneModeCalls;
    g_op24LoadingGateCalls = previousLoadingGateCalls;

    // The native handler has already accepted and applied this packet. Cache
    // only the complete local mode1/mode3 stat block for the Lua calculator;
    // remote town actors and incomplete bodies are deliberately ignored.
    CacheCurrentCharacterStatsFromClass0Packet(packet);

    unsigned int opcode = 0;
    unsigned int packetLength = 0;
    unsigned int recordCount = 0;
    unsigned int scene = 0;
    unsigned int recordKind = 0;
    unsigned int recordObjectKey = 0;
    unsigned int recordOwnerContext = 0;
    unsigned int memberCount = 0;
    bool recordContainsSelfSlot = false;
    __try {
        if (packet) {
            const unsigned char* bytes = reinterpret_cast<const unsigned char*>(packet);
            opcode = *reinterpret_cast<const unsigned short*>(bytes + 1);
            packetLength = *reinterpret_cast<const unsigned int*>(bytes + 3);
            if (opcode == kPartyActorProjectionOpcode && packetLength >= 26) {
                recordCount = *reinterpret_cast<const unsigned short*>(bytes + 16);
                scene = *reinterpret_cast<const unsigned short*>(bytes + 18);
                recordObjectKey = *reinterpret_cast<const unsigned short*>(bytes + 20);
                recordOwnerContext = bytes[23];
                recordKind = bytes[25];

                // The current compatibility profile's kind-0 row contains an
                // outer dstr at body+12, 13 fixed bytes, then the party slots.
                // Every slot carries the cross-channel identity block before
                // its three state bytes. Walk the bounded row so a native
                // pointer repair is allowed only when this actor's own object
                // key is actually present in the received party table.
                const size_t bodyLength = packetLength - 16;
                const unsigned char* body = bytes + 16;
                if (recordKind == 0 && bodyLength >= 33) {
                    size_t offset = 12;
                    unsigned int outerNameLength =
                        *reinterpret_cast<const unsigned int*>(body + offset);
                    offset += 4;
                    if (outerNameLength <= bodyLength - offset) {
                        offset += outerNameLength;
                        if (offset <= bodyLength && bodyLength - offset >= 14) {
                            offset += 13;
                            memberCount = body[offset++];
                            if (memberCount <= 8) {
                                bool bounded = true;
                                for (unsigned int i = 0; i < memberCount; ++i) {
                                    if (bodyLength - offset < 10) {
                                        bounded = false;
                                        break;
                                    }
                                    offset += 1;
                                    unsigned int memberObjectKey =
                                        *reinterpret_cast<const unsigned short*>(body + offset);
                                    offset += 2;
                                    offset += 4;
                                    unsigned int memberNameLength =
                                        *reinterpret_cast<const unsigned int*>(body + offset);
                                    offset += 4;
                                    if (memberNameLength > bodyLength - offset ||
                                        bodyLength - offset - memberNameLength < 3) {
                                        bounded = false;
                                        break;
                                    }
                                    offset += memberNameLength;
                                    offset += 3;
                                    if (memberObjectKey == recordObjectKey) {
                                        recordContainsSelfSlot = true;
                                    }
                                }
                                if (!bounded) {
                                    memberCount = 0;
                                    recordContainsSelfSlot = false;
                                }
                            }
                        }
                    }
                }
            }
        }

        if (opcode == kPartyActorProjectionOpcode &&
            recordCount != 0 &&
            scene == kPartyActorProjectionScene &&
            recordKind == 0 &&
            InterlockedCompareExchange(&g_partyHudRefreshPending, 0, 1) == 1) {
            uintptr_t objectManager =
                *reinterpret_cast<uintptr_t*>(g_dnfBase + kObjectManagerPointerRva);
            uintptr_t sceneActorManager =
                *reinterpret_cast<uintptr_t*>(
                    g_dnfBase + kSceneActorManagerPointerRva);
            CurrentActorFn currentActorFn = reinterpret_cast<CurrentActorFn>(
                g_dnfBase + kCurrentActorRva);
            ActorByObjectKeyFn actorByObjectKeyFn = reinterpret_cast<ActorByObjectKeyFn>(
                g_dnfBase + kActorByObjectKeyRva);
            ActorByContextFn actorByContextFn = reinterpret_cast<ActorByContextFn>(
                g_dnfBase + kActorByContextRva);
            ResolveActorInContextFn resolveActorInContextFn =
                reinterpret_cast<ResolveActorInContextFn>(
                    g_dnfBase + kResolveActorInContextRva);
            uintptr_t currentActor = objectManager && currentActorFn
                ? reinterpret_cast<uintptr_t>(
                    currentActorFn(reinterpret_cast<void*>(objectManager)))
                : 0;
            uintptr_t localMemberActor = recordObjectKey && actorByObjectKeyFn
                ? reinterpret_cast<uintptr_t>(
                    actorByObjectKeyFn(static_cast<unsigned short>(recordObjectKey)))
                : 0;
            uintptr_t contextMemberActor =
                recordObjectKey && actorByContextFn
                ? reinterpret_cast<uintptr_t>(
                    actorByContextFn(
                        static_cast<unsigned short>(recordObjectKey),
                        static_cast<int>(recordOwnerContext)))
                : 0;
            uintptr_t recordActor =
                sceneActorManager && recordObjectKey && resolveActorInContextFn
                ? reinterpret_cast<uintptr_t>(
                    resolveActorInContextFn(
                        reinterpret_cast<void*>(sceneActorManager),
                        static_cast<unsigned char>(recordOwnerContext),
                        static_cast<unsigned short>(recordObjectKey)))
                : 0;
            uintptr_t contextPartyOwnerBefore = contextMemberActor
                ? *reinterpret_cast<uintptr_t*>(
                    contextMemberActor + kActorPartyOwnerOffset)
                : 0;
            uintptr_t hudPartyOwnerBefore = currentActor
                ? *reinterpret_cast<uintptr_t*>(
                    currentActor + kActorPartyOwnerOffset)
                : 0;
            bool repairedContextPartyOwner = false;
            if (currentActor &&
                localMemberActor == currentActor &&
                contextMemberActor &&
                recordActor &&
                recordContainsSelfSlot &&
                contextPartyOwnerBefore == 0) {
                // sub_269EE50's official invariant is memberActor+0x498 =
                // the outer party record resolved by sub_26A2000(ownerContext,
                // objectKey). With this profile's context-0 scene record and a
                // nonzero selected channel, the native function resolves that
                // member through sub_17B6AA0(objectKey, ownerContext), not
                // sub_2699A30. Restore that exact validated self-member target.
                *reinterpret_cast<uintptr_t*>(
                    contextMemberActor + kActorPartyOwnerOffset) = recordActor;
                repairedContextPartyOwner = true;
            }
            bool repairedHudPartyOwner = false;
            hudPartyOwnerBefore = currentActor
                ? *reinterpret_cast<uintptr_t*>(
                    currentActor + kActorPartyOwnerOffset)
                : 0;
            if (currentActor &&
                localMemberActor == currentActor &&
                recordActor &&
                recordContainsSelfSlot &&
                hudPartyOwnerBefore == 0) {
                // The context-0 compatibility remap keeps the visible local
                // actor in sub_2699A30 as well. Its HUD state-1 path reads the
                // same +0x498 field, so preserve that existing compatibility
                // binding when the exact context member is a separate object.
                *reinterpret_cast<uintptr_t*>(
                    currentActor + kActorPartyOwnerOffset) = recordActor;
                repairedHudPartyOwner = true;
            }
            uintptr_t contextPartyOwnerAfter = contextMemberActor
                ? *reinterpret_cast<uintptr_t*>(
                    contextMemberActor + kActorPartyOwnerOffset)
                : 0;
            uintptr_t hudPartyOwnerAfter = currentActor
                ? *reinterpret_cast<uintptr_t*>(
                    currentActor + kActorPartyOwnerOffset)
                : 0;
            LogLine("party-hud actor bind opcode=%u packet_len=%u "
                "record_key=%u owner_context=%u members=%u self_slot=%u "
                "current_actor=%p local_member_actor=%p "
                "context_member_actor=%p record_actor=%p "
                "context_owner_before=%p context_owner_after=%p "
                "hud_owner_before=%p hud_owner_after=%p "
                "context_repaired=%u hud_repaired=%u",
                opcode, packetLength, recordObjectKey, recordOwnerContext,
                memberCount,
                recordContainsSelfSlot ? 1u : 0u,
                reinterpret_cast<void*>(currentActor),
                reinterpret_cast<void*>(localMemberActor),
                reinterpret_cast<void*>(contextMemberActor),
                reinterpret_cast<void*>(recordActor),
                reinterpret_cast<void*>(contextPartyOwnerBefore),
                reinterpret_cast<void*>(contextPartyOwnerAfter),
                reinterpret_cast<void*>(hudPartyOwnerBefore),
                reinterpret_cast<void*>(hudPartyOwnerAfter),
                repairedContextPartyOwner ? 1u : 0u,
                repairedHudPartyOwner ? 1u : 0u);

            uintptr_t sceneRoot =
                *reinterpret_cast<uintptr_t*>(g_dnfBase + kSceneRootPointerRva);
            SceneHudContextFn contextFn = reinterpret_cast<SceneHudContextFn>(
                g_dnfBase + kSceneHudContextRva);
            PartyHudRefreshFn refreshFn = reinterpret_cast<PartyHudRefreshFn>(
                g_dnfBase + kPartyHudRefreshRva);
            void* hudContext = sceneRoot && contextFn
                ? contextFn(reinterpret_cast<void*>(sceneRoot))
                : nullptr;
            if (hudContext && refreshFn) {
                refreshFn(hudContext, 1);
                LONG hit = InterlockedIncrement(&g_partyHudRefreshApplyCount);
                LogLine("party-hud native refresh applied hit=%ld opcode=%u "
                    "packet_len=%u records=%u scene=%u kind=%u "
                    "source=successful-class1-op12-next-class0-op9",
                    hit, opcode, packetLength, recordCount, scene, recordKind);
            } else {
                InterlockedExchange(&g_partyHudRefreshPending, 1);
                LogLine("party-hud native refresh deferred opcode=%u "
                    "packet_len=%u scene_root=%p hud_context=%p",
                    opcode, packetLength, reinterpret_cast<void*>(sceneRoot),
                    hudContext);
            }
        }
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        InterlockedExchange(&g_partyHudRefreshPending, 1);
        LogLine("party-hud native refresh exception opcode=%u packet_len=%u "
            "records=%u scene=%u kind=%u code=0x%08X pending_restored=1",
            opcode, packetLength, recordCount, scene, recordKind,
            GetExceptionCode());
    }

    TraceDispatchState("after", 0, packet);
    return result;
}

void CachePartyHudRefreshFromClass1Packet(int packet)
{
    if (!packet || !g_dnfBase) return;

    unsigned int opcode = 0;
    unsigned int packetLength = 0;
    unsigned int success = 0;
    __try {
        const unsigned char* bytes = reinterpret_cast<const unsigned char*>(packet);
        opcode = *reinterpret_cast<const unsigned short*>(bytes + 1);
        if (opcode != kSetPartyInfoOpcode) return;

        packetLength = *reinterpret_cast<const unsigned int*>(bytes + 3);
        if (packetLength < 17) {
            LogLine("party-hud refresh ACK rejected opcode=%u packet_len=%u "
                "reason=short-packet",
                opcode, packetLength);
            return;
        }

        success = bytes[16];
        if (success != 1) {
            InterlockedExchange(&g_partyHudRefreshPending, 0);
            LogLine("party-hud refresh ACK rejected opcode=%u packet_len=%u "
                "success=%u reason=server-rejected-set-party-info",
                opcode, packetLength, success);
            return;
        }

        InterlockedExchange(&g_partyHudRefreshPending, 1);
        LogLine("party-hud refresh armed opcode=%u packet_len=%u success=%u "
            "target=next-class0-op9",
            opcode, packetLength, success);
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        InterlockedExchange(&g_partyHudRefreshPending, 0);
        LogLine("party-hud refresh ACK cache exception opcode=%u packet_len=%u "
            "success=%u code=0x%08X",
            opcode, packetLength, success, GetExceptionCode());
    }
}

bool ApplyPersistentAuraSkinState(const char* source)
{
    LONG entitlement = InterlockedCompareExchange(
        &g_auraSkinEntitlementState, 0, 0);
    if (!g_dnfBase || entitlement < 0) return false;

    uintptr_t objectManager = 0;
    void* actor = nullptr;
    void* stateOwner = nullptr;
    void* stateBlock = nullptr;
    LONG previous = 0;
    LONG updated = 0;
    __try {
        objectManager = *reinterpret_cast<uintptr_t*>(
            g_dnfBase + kObjectManagerPointerRva);
        CurrentActorFn currentActorFn = reinterpret_cast<CurrentActorFn>(
            g_dnfBase + kCurrentActorRva);
        actor = objectManager && currentActorFn
            ? currentActorFn(reinterpret_cast<void*>(objectManager))
            : nullptr;
        if (!actor) return false;

        stateOwner = *reinterpret_cast<void**>(
            reinterpret_cast<uintptr_t>(actor) +
            kAuraSkinActorStateOwnerOffset);
        if (!stateOwner) return false;
        uintptr_t vtable = *reinterpret_cast<uintptr_t*>(stateOwner);
        if (!vtable) return false;
        AuraSkinStateResolverFn resolveState =
            *reinterpret_cast<AuraSkinStateResolverFn*>(
                vtable + kAuraSkinStateResolverVtableOffset);
        stateBlock = resolveState ? resolveState(stateOwner) : nullptr;
        if (!stateBlock) return false;

        volatile LONG* flags = reinterpret_cast<volatile LONG*>(
            reinterpret_cast<uintptr_t>(stateBlock) +
            kAuraSkinStateFlagsOffset);
        previous = InterlockedCompareExchange(flags, 0, 0);
        for (;;) {
            updated = entitlement != 0
                ? previous | static_cast<LONG>(kAuraSkinUnlockedMask)
                : previous & ~static_cast<LONG>(kAuraSkinUnlockedMask);
            LONG observed = InterlockedCompareExchange(flags, updated, previous);
            if (observed == previous) break;
            previous = observed;
        }
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("aura-skin persistent state exception state=%ld source=%s "
            "actor=%p owner=%p block=%p code=0x%08X",
            entitlement, source ? source : "unknown", actor, stateOwner,
            stateBlock, GetExceptionCode());
        return false;
    }

    LONG hit = InterlockedIncrement(&g_auraSkinDeferredApplyCount);
    LogLine("aura-skin persistent state applied hit=%ld state=%ld changed=%d "
        "previous=0x%08X updated=0x%08X source=%s actor=%p owner=%p block=%p",
        hit, entitlement, previous != updated ? 1 : 0,
        static_cast<unsigned int>(previous),
        static_cast<unsigned int>(updated),
        source ? source : "unknown", actor, stateOwner, stateBlock);
    return true;
}

bool CacheAuraSkinPersistentStateFromClass1Packet(int packet)
{
    if (!packet || !g_dnfBase) return false;

    unsigned int opcode = 0;
    unsigned int packetLength = 0;
    unsigned int success = 0;
    __try {
        const unsigned char* bytes = reinterpret_cast<const unsigned char*>(packet);
        opcode = *reinterpret_cast<const unsigned short*>(bytes + 1);
        if (opcode != kAuraSkinSlotOpcode) return false;

        packetLength = *reinterpret_cast<const unsigned int*>(bytes + 3);
        if (packetLength != kAuraSkinSilentRestorePacketLength) return false;

        success = bytes[16];
        if (success > 1 ||
            bytes[17] != 'A' || bytes[18] != 'U' ||
            bytes[19] != 'R' || bytes[20] != 'A') {
            return false;
        }

        InterlockedExchange(&g_auraSkinEntitlementState,
            static_cast<LONG>(success));
        LONG hit = InterlockedIncrement(&g_auraSkinSilentRestoreCount);
        bool applied = ApplyPersistentAuraSkinState(
            "class1-op863-marked-state");
        LogLine("aura-skin persistent state cached hit=%ld opcode=%u "
            "packet_len=%u state=%u applied=%d source=class1-op863-marker",
            hit, opcode, packetLength, success, applied ? 1 : 0);
        return true;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("aura-skin persistent state cache exception opcode=%u "
            "packet_len=%u state=%u code=0x%08X",
            opcode, packetLength, success, GetExceptionCode());
        return false;
    }
}

int __stdcall ProxyClass1Dispatch(int packet, int a2, int a3)
{
    TraceDispatchState("before", 1, packet);
    if (CacheAuraSkinPersistentStateFromClass1Packet(packet)) {
        TraceDispatchState("after-persistent-aura-state", 1, packet);
        return 1;
    }
    Class1DispatchFn original = reinterpret_cast<Class1DispatchFn>(g_originalClass1Dispatch);
    int result = original ? original(packet, a2, a3) : 0;
    TraceDispatchState("after", 1, packet);
    return result;
}

bool SendCurrentPartyDirectoryRefresh()
{
    if (!g_dnfBase) return false;
    __try {
        UpperWriterValueFn value = reinterpret_cast<UpperWriterValueFn>(
            g_dnfBase + kUpperWriterValueRva);
        UpperWriterScalarFn writeU16 = reinterpret_cast<UpperWriterScalarFn>(
            g_dnfBase + kUpperWriterU16Rva);
        UpperWriterScalarFn writeU8 = reinterpret_cast<UpperWriterScalarFn>(
            g_dnfBase + kUpperWriterU8Rva);
        UpperWriterFlushFn flush = reinterpret_cast<UpperWriterFlushFn>(
            g_dnfBase + kUpperWriterFlushRva);

        // Exact current-EXE writer sequence at 0x032289E4:
        // upper_u16(98), upper_u8(0), upper_flush().
        writeU16(value(), kPartyDirectoryRefreshOpcode);
        writeU8(value(), kPartyDirectoryRefreshModeFull);
        flush();
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("party-directory refresh request exception opcode=%u mode=%u code=0x%08X",
            kPartyDirectoryRefreshOpcode,
            static_cast<unsigned int>(kPartyDirectoryRefreshModeFull),
            GetExceptionCode());
        return false;
    }
    LogLine("party-directory refresh request sent opcode=%u mode=%u source=owner9-open",
        kPartyDirectoryRefreshOpcode,
        static_cast<unsigned int>(kPartyDirectoryRefreshModeFull));
    return true;
}

bool __fastcall ProxyJoustSceneBlockCheck(void* self, void* /*unused*/)
{
    JoustSceneBlockCheckFn original = reinterpret_cast<JoustSceneBlockCheckFn>(
        g_originalJoustSceneBlockCheck);
    bool blocked = original ? original(self) : true;
    uintptr_t caller = reinterpret_cast<uintptr_t>(_ReturnAddress());
    uintptr_t callerRva = caller >= g_dnfBase ? caller - g_dnfBase : 0;
    if (callerRva != kJoustOpenGateReturnRva) {
        return blocked;
    }

    // The owner-609 gate itself performs the remaining native checks: it
    // requests op1291, verifies the type-73 activity state, constructs the
    // native controls, and only then lets sub_2FDD1A0 promote the owner.
    LogLine("joust scene-state gate caller=+0x%08X native_blocked=%d override=allow owner=%u",
        static_cast<unsigned int>(callerRva), blocked ? 1 : 0,
        static_cast<unsigned int>(kJoustUiOwner));
    return false;
}

void* __fastcall ProxyAuraSkinSceneUiOpen(
    void* self, void* /*unused*/, uintptr_t uiOwner, int mode, unsigned int group)
{
    if (uiOwner == kAvatarPanelUiOwner) {
        ApplyPersistentAuraSkinState("avatar-panel-before-create");
    }
    SceneUiOpenFn original = reinterpret_cast<SceneUiOpenFn>(g_originalSceneUiOpen);
    uintptr_t caller = reinterpret_cast<uintptr_t>(_ReturnAddress());
    void* result = original ? original(self, uiOwner, mode, group) : nullptr;
    if (uiOwner == kJoustUiOwner) {
        SceneUiIsOpenFn isOpen = g_dnfBase
            ? reinterpret_cast<SceneUiIsOpenFn>(g_dnfBase + kSceneUiIsOpenRva)
            : nullptr;
        bool active = false;
        __try {
            active = isOpen && isOpen(self, uiOwner);
        }
        __except (EXCEPTION_EXECUTE_HANDLER) {
            LogLine("joust scene-ui state read exception code=0x%08X",
                GetExceptionCode());
        }
        LogLine("joust scene-ui open caller=+0x%08X owner=%u mode=%d group=%u result=%p active=%d",
            caller >= g_dnfBase
                ? static_cast<unsigned int>(caller - g_dnfBase)
                : 0,
            static_cast<unsigned int>(uiOwner), mode, group, result,
            active ? 1 : 0);
    }
    if (!result) {
        return result;
    }

    if (uiOwner == kPartyDirectoryUiOwner) {
        SendCurrentPartyDirectoryRefresh();
    }
    return result;
}

void* __fastcall ProxySceneUiOpen(
    void* self, void* /*unused*/, uintptr_t uiOwner, int mode, unsigned int group)
{
    if (uiOwner == 0x298) {
        uintptr_t caller = reinterpret_cast<uintptr_t>(_ReturnAddress());
        char preview[192] = { 0 };
        unsigned int previewLength = 0;
        unsigned int previewError = 0;
        __try {
            const wchar_t* text = reinterpret_cast<const wchar_t*>(
                static_cast<uintptr_t>(static_cast<unsigned int>(mode)));
            wchar_t widePreview[64] = { 0 };
            while (text && previewLength + 1 < _countof(widePreview) &&
                text[previewLength] != L'\0') {
                widePreview[previewLength] = text[previewLength];
                ++previewLength;
            }
            if (previewLength != 0) {
                WideCharToMultiByte(
                    CP_UTF8, 0, widePreview, static_cast<int>(previewLength),
                    preview, static_cast<int>(sizeof(preview) - 1),
                    nullptr, nullptr);
            }
        }
        __except (EXCEPTION_EXECUTE_HANDLER) {
            previewError = GetExceptionCode();
        }
        LogLine("scene-ui announcement open caller=+0x%08X owner=%u "
            "text_ptr=%p text_len=%u text=%s group=%u read_error=0x%08X",
            caller >= g_dnfBase
                ? static_cast<unsigned int>(caller - g_dnfBase)
                : 0,
            static_cast<unsigned int>(uiOwner),
            reinterpret_cast<void*>(
                static_cast<uintptr_t>(static_cast<unsigned int>(mode))),
            previewLength, preview, group, previewError);
    }

    if (uiOwner == kAvatarPanelUiOwner) {
        ApplyPersistentAuraSkinState("avatar-panel-before-create-debug");
    }
    SceneUiOpenFn original = reinterpret_cast<SceneUiOpenFn>(g_originalSceneUiOpen);
    void* result = original ? original(self, uiOwner, mode, group) : nullptr;
    return result;
}

int __cdecl ProxyLocalActorCreate(int objectKey)
{
    LogLine("local-actor-create phase=before object_key=%u", static_cast<unsigned int>(objectKey) & 0xFFFFu);
    LocalActorCreateFn original = reinterpret_cast<LocalActorCreateFn>(g_originalLocalActorCreate);
    int result = original ? original(objectKey) : 0;

    uintptr_t objectManager = 0;
    uintptr_t currentActor = 0;
    uintptr_t controlledActor = 0;
    __try {
        objectManager = *reinterpret_cast<uintptr_t*>(g_dnfBase + kObjectManagerPointerRva);
        controlledActor = *reinterpret_cast<uintptr_t*>(g_dnfBase + kControlledActorPointerRva);
        CurrentActorFn currentActorFn = reinterpret_cast<CurrentActorFn>(g_dnfBase + kCurrentActorRva);
        currentActor = objectManager && currentActorFn
            ? reinterpret_cast<uintptr_t>(currentActorFn(reinterpret_cast<void*>(objectManager)))
            : 0;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("local-actor-create state read exception object_key=%u code=0x%08X",
            static_cast<unsigned int>(objectKey) & 0xFFFFu, GetExceptionCode());
    }

    LogLine("local-actor-create phase=after object_key=%u result=%p current_actor=%p controlled_actor=%p",
        static_cast<unsigned int>(objectKey) & 0xFFFFu, reinterpret_cast<void*>(result),
        reinterpret_cast<void*>(currentActor), reinterpret_cast<void*>(controlledActor));
    return result;
}

void __cdecl TraceMode0OwnerCompare(unsigned int sceneChannel, unsigned int localOwnerChannel)
{
    bool currentSceneContextRemap =
        sceneChannel != 0 && localOwnerChannel == 0;
    LogLine("mode0-owner-compare scene_channel=%u local_owner_channel=%u branch=%s",
        sceneChannel, localOwnerChannel,
        sceneChannel == localOwnerChannel || currentSceneContextRemap
            ? (currentSceneContextRemap ? "current-scene-context0-remap" : "current-scene-create")
            : "remote-actor");
}

// The local bridge's context-0 actor rows belong to the currently selected
// scene. Once the real HUD channel index is restored, translate only that
// established context-0 sentinel to the current scene branch. Every nonzero
// owner context retains the native CMP/JZ result.
__declspec(naked) void ProxyMode0OwnerCompare()
{
    __asm {
        pushfd
        pushad
        push ecx
        push esi
        call TraceMode0OwnerCompare
        add esp, 8
        popad
        popfd

        cmp esi, ecx
        je mode0_owner_local
        test ecx, ecx
        jnz mode0_owner_remote
        test esi, esi
        jnz mode0_owner_local

    mode0_owner_remote:
        jmp dword ptr [g_mode0OwnerRemoteResume]

    mode0_owner_local:
        jmp dword ptr [g_mode0OwnerLocalResume]
    }
}

void __cdecl TraceMode3OwnerResolve(unsigned int currentSceneChannel, unsigned int packetOwnerContext)
{
    bool currentSceneContextRemap =
        currentSceneChannel != 0 && packetOwnerContext == 0;
    LogLine("mode3-owner-resolve scene_channel=%u packet_owner=%u branch=%s",
        currentSceneChannel, packetOwnerContext,
        currentSceneChannel == packetOwnerContext || currentSceneContextRemap
            ? (currentSceneContextRemap ? "current-scene-context0-remap" : "current-scene")
            : "remote-actor");
}

// Resolve the actor with the same context-0/current-scene translation as mode
// 0.  The second comparison below must make the same decision.
__declspec(naked) void ProxyMode3OwnerResolve()
{
    __asm {
        pushfd
        pushad
        push ecx
        push esi
        call TraceMode3OwnerResolve
        add esp, 8
        popad
        popfd

        cmp ecx, esi
        je mode3_owner_local
        test ecx, ecx
        jnz mode3_owner_remote
        test esi, esi
        jnz mode3_owner_local

    mode3_owner_remote:
        // Complete the displaced remote-path instruction before resuming.
        mov esi, dword ptr [ebp-4B0h]
        jmp dword ptr [g_mode3OwnerRemoteResume]

    mode3_owner_local:
        jmp dword ptr [g_mode3OwnerLocalResume]
    }
}

__declspec(naked) void ProxyMode3OwnerFinalize()
{
    __asm {
        mov ecx, dword ptr [g_mode3LocalOwnerChannelAddress]
        movzx eax, byte ptr [ecx]
        test eax, eax
        jz mode3_finalize_local
        cmp eax, ebx
        je mode3_finalize_local

        jmp dword ptr [g_mode3OwnerFinalizeRemoteResume]

    mode3_finalize_local:
        jmp dword ptr [g_mode3OwnerFinalizeLocalResume]
    }
}

void* EnsureDungeonPickupContainer(void* actor, const char* source)
{
    if (!actor || !g_dnfBase || g_pickupContainerRepairActive) return nullptr;

    void** containerSlot = reinterpret_cast<void**>(
        reinterpret_cast<uintptr_t>(actor) + kPickupContainerOffset);
    void* container = nullptr;
    unsigned int townEntryState = 0;
    void* controlledActor = nullptr;

    __try {
        container = *containerSlot;
        if (container) return container;

        controlledActor = *reinterpret_cast<void**>(
            g_dnfBase + kControlledActorPointerRva);
        townEntryState = *reinterpret_cast<unsigned int*>(
            g_dnfBase + kTownEntryStateRva);
        if (controlledActor != actor || townEntryState != kDungeonTownEntryState) {
            return nullptr;
        }

        g_pickupContainerRepairActive = true;
        PickupContainerAllocatorFn allocator =
            reinterpret_cast<PickupContainerAllocatorFn>(
                g_dnfBase + kPickupContainerAllocatorRva);
        PickupContainerConstructorFn constructor =
            reinterpret_cast<PickupContainerConstructorFn>(
                g_dnfBase + kPickupContainerConstructorRva);
        void* memory = allocator ? allocator(kPickupContainerBytes) : nullptr;
        if (!memory) {
            g_pickupContainerRepairActive = false;
            LogLine("pickup-container repair failed source=%s actor=%p reason=allocation",
                source ? source : "unknown", actor);
            return nullptr;
        }

        container = constructor
            ? constructor(memory, 0, kPickupContainerKind)
            : nullptr;
        if (!container) {
            g_pickupContainerRepairActive = false;
            LogLine("pickup-container repair failed source=%s actor=%p reason=constructor",
                source ? source : "unknown", actor);
            return nullptr;
        }

        // The native actor initializer owns this same field and the matching
        // teardown paths destroy it. Pickup runs on the game thread, so install
        // it only after the guarded null check and let native ownership resume.
        *containerSlot = container;
        g_pickupContainerRepairActive = false;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        g_pickupContainerRepairActive = false;
        LogLine("pickup-container repair exception source=%s actor=%p code=0x%08X",
            source ? source : "unknown", actor, GetExceptionCode());
        return nullptr;
    }

    LONG repair = InterlockedIncrement(&g_pickupContainerRepairCount);
    LogLine("pickup-container repaired source=%s actor=%p container=%p state=%u repair=%ld",
        source ? source : "unknown", actor, container, townEntryState, repair);
    return container;
}

void* __fastcall ProxyPickupContainerGetter(void* self, void* /*unused*/)
{
    PickupContainerGetterFn original =
        reinterpret_cast<PickupContainerGetterFn>(g_originalPickupContainerGetter);
    // sub_24A1BC0 is a general actor-container getter.  It is reached while a
    // town actor is being installed as well as by the dungeon pickup path, and
    // the current EXE reports town-entry state 3 in both situations.  Creating
    // a replacement container here splits dword_51B2768 from the UI alias at
    // dword_51B2790 and leaves the crystal/soul warehouse counters reading an
    // empty manager.  Keep this proxy side-effect free; the narrowly scoped
    // sub_24B2840 hook below owns the missing-container repair immediately
    // before an actual manual pickup.
    return original ? original(self) : nullptr;
}

void* __fastcall ProxyManualPickup(void* self, void* /*unused*/, void* objects)
{
    EnsureDungeonPickupContainer(self, "manual-pickup");
    ManualPickupFn original = reinterpret_cast<ManualPickupFn>(g_originalManualPickup);
    return original ? original(self, objects) : nullptr;
}

// Preserve sub_1D84FE0's native stack contract. The wide-name pointer was
// already placed on the stack before the equipped-creature key lookup. The
// original add esp,4 removes only sub_25FD8D0's cdecl key argument; on a
// successful lookup execution resumes at the native sub_34DEA20 call, while a
// missing map entry skips that optional third copy and returns through the
// original epilogue. The actor and live creature name copies earlier in the
// same handler remain untouched.
__declspec(naked) void ProxyCreatureRenameMapNullCheck()
{
    __asm {
        add esp, 4
        test eax, eax
        jz creature_rename_map_done
        lea ecx, [eax+14h]
        jmp dword ptr [g_creatureRenameMapUpdateResume]

    creature_rename_map_done:
        jmp dword ptr [g_creatureRenameMapDoneResume]
    }
}

// Entry contract at sub_1D73120+0x899 after the native same-template path has
// applied the row's serial/count:
//   esi = existing item object
//   ebp-0x17D = list type
//   ebp-0x17C = slot
//   ebp-0x104 = complete dynamic state parsed from the incoming 0x77 row
// Invoke only for list-7 creatures or the equipped creature at list-3/slot-26.
// The native dynamic-state setter is already used later in this same handler.
// The displaced movsx is restored before the original slot-refresh call, and
// the existing item pointer is never destroyed or replaced.
__declspec(naked) void ProxyPetItemUpdateDynamicState()
{
    __asm {
        cmp byte ptr [ebp-17Dh], 7
        je pet_item_update_apply
        cmp byte ptr [ebp-17Dh], 3
        jne pet_item_update_resume
        cmp word ptr [ebp-17Ch], 26
        jne pet_item_update_resume

    pet_item_update_apply:
        mov edx, [esi]
        lea eax, [ebp-104h]
        push eax
        mov ecx, esi
        call dword ptr [edx+158h]

    pet_item_update_resume:
        movsx eax, word ptr [ebp-17Ch]
        jmp dword ptr [g_petItemUpdateDynamicStateResume]
    }
}

void __cdecl TraceAutoRepairAuxContainerMissing(void* actor)
{
    LONG hit = InterlockedIncrement(&g_autoRepairAuxNullCount);
    if (hit <= 8) {
        LogLine("auto-repair auxiliary equipment scan skipped actor=%p reason=null_container hit=%ld",
            actor, hit);
    }
}

// Entry contract at sub_20695F0+0x18F:
//   eax = sub_24A1BC0(controlled_actor)
//   [esp] = auxiliary slot index already pushed by the native loop
// A present container follows the exact native sub_2326860 call and resumes at
// 0x2069786. A missing optional container discards that pending argument and
// skips only the auxiliary slots; the ordinary equipment durability pass has
// already completed.
__declspec(naked) void ProxyAutoRepairAuxContainerLookup()
{
    __asm {
        test eax, eax
        jz auto_repair_aux_missing
        mov ecx, eax
        call dword ptr [g_autoRepairAuxLookupFn]
        jmp dword ptr [g_autoRepairAuxLookupResume]

    auto_repair_aux_missing:
        pushfd
        pushad
        push edi
        call TraceAutoRepairAuxContainerMissing
        add esp, 4
        popad
        popfd
        add esp, 4
        jmp dword ptr [g_autoRepairAuxLoopDone]
    }
}

int __fastcall ProxyOp24SceneMode(void* self, void* /*unused*/)
{
    Op24SceneModeFn original = reinterpret_cast<Op24SceneModeFn>(g_originalOp24SceneMode);
    int result = original ? original(self) : 0;
    if (g_traceOp24) {
        if (result == 5) g_op24SceneModeEarlyReturn = true;
        if (g_op24SceneModeCalls++ == 0) {
            LogLine("op24-native-gate name=scene_mode self=%p result=%d early_return=%d",
                self, result, result == 5 ? 1 : 0);
        }
    }
    return result;
}

unsigned char __cdecl ProxyOp24LoadingGate()
{
    Op24LoadingGateFn original = reinterpret_cast<Op24LoadingGateFn>(g_originalOp24LoadingGate);
    unsigned char result = original ? original() : 0;
    if (g_traceOp24) {
        if (result != 0) g_op24LoadingGateEarlyReturn = true;
        if (g_op24LoadingGateCalls++ == 0) {
            LogLine("op24-native-gate name=loading_gate result=%u early_return=%d",
                static_cast<unsigned int>(result), result ? 1 : 0);
        }
    }
    return result;
}

bool TryCopyTraceBytes(const void* source, void* destination, size_t length)
{
    if (!source || !destination || length == 0) return false;
    __try {
        const unsigned char* from = static_cast<const unsigned char*>(source);
        unsigned char* to = static_cast<unsigned char*>(destination);
        for (size_t i = 0; i < length; ++i) to[i] = from[i];
        return true;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        return false;
    }
}

void FormatTraceBytes(const unsigned char* bytes, size_t length, char* output, size_t outputSize)
{
    if (!output || outputSize == 0) return;
    output[0] = '\0';
    if (!bytes || length == 0) return;

    static const char kHex[] = "0123456789ABCDEF";
    size_t used = 0;
    for (size_t i = 0; i < length && used + 3 < outputSize; ++i) {
        unsigned char value = bytes[i];
        output[used++] = kHex[value >> 4];
        output[used++] = kHex[value & 0x0F];
        if (i + 1 < length && used + 1 < outputSize) output[used++] = ' ';
    }
    output[used] = '\0';
}

bool IsSocketTraceEnabled()
{
    wchar_t path[MAX_PATH] = { 0 };
    if (!GetModuleFileNameW(nullptr, path, MAX_PATH)) return false;
    wchar_t* slash = wcsrchr(path, L'\\');
    if (!slash) return false;
    slash[1] = L'\0';
    wcsncat_s(path, MAX_PATH, L"90CN_socket_trace.ini", _TRUNCATE);
    return GetPrivateProfileIntW(L"trace", L"enabled", 0, path) != 0;
}

void CacheCurrentItemRow(uintptr_t itemObject, const unsigned char* raw)
{
    if (!itemObject || !raw) return;
    LONG sequence = InterlockedIncrement(&g_socketTraceRowSequence);
    LONG slot = sequence % kSocketTraceCacheRows;
    if (slot < 0) slot += kSocketTraceCacheRows;

    AcquireSRWLockExclusive(&g_socketTraceLock);
    g_socketTraceRows[slot].sequence = sequence;
    g_socketTraceRows[slot].parserObject = itemObject;
    memcpy(g_socketTraceRows[slot].raw, raw, kCurrentItemRawBytes);
    ReleaseSRWLockExclusive(&g_socketTraceLock);
}

uint16_t CurrentItemRowSlot(const unsigned char* raw)
{
    if (!raw) return 0;
    return static_cast<uint16_t>(raw[0]) |
        (static_cast<uint16_t>(raw[1]) << 8);
}

uint32_t CurrentItemRowTemplateID(const unsigned char* raw)
{
    if (!raw) return 0;
    return static_cast<uint32_t>(raw[2]) |
        (static_cast<uint32_t>(raw[3]) << 8) |
        (static_cast<uint32_t>(raw[4]) << 16) |
        (static_cast<uint32_t>(raw[5]) << 24);
}

uint32_t CurrentItemRowIdentity(const unsigned char* raw)
{
    if (!raw) return 0;
    return static_cast<uint32_t>(raw[kCurrentItemIdentityOffset]) |
        (static_cast<uint32_t>(raw[kCurrentItemIdentityOffset + 1]) << 8) |
        (static_cast<uint32_t>(raw[kCurrentItemIdentityOffset + 2]) << 16) |
        (static_cast<uint32_t>(raw[kCurrentItemIdentityOffset + 3]) << 24);
}

bool IsCurrentPremiumContractID(uint32_t templateID)
{
    // Generated from the pinned runtime Script.pvf
    // etc/premiumlist_new.etc [item] rows.  Keeping the allow-list in this
    // current-client compatibility unit prevents an all-stackable bypass.
    static const uint32_t kContractIDs[] = {
        30, 31, 32, 33, 34, 43, 44, 45, 46,
        193, 194, 196, 198, 720, 741, 742, 743, 744, 820, 901, 917, 920,
        2660012, 2660013, 2660050, 2660051, 2660290, 2660354, 2660358,
        2660409, 2660410, 2660411, 2660543, 2660545, 2660703, 2660704,
        2660705, 2682763, 2683452, 2683453, 2683454, 2683455, 2683456,
        2683457, 2683458, 2683459, 2749321,
        10000388, 10000389, 10000390, 10000391,
        10012576, 10012577, 10012578, 10012579,
        10092497, 10096109, 10096110, 10096111, 10096112, 10096113,
        10151653, 10157955,
        490700021, 490700022, 490700023,
        490700028, 490700029, 490700030, 490700031, 490700032,
        490700033, 490700034, 490700035, 490700036
    };
    for (size_t i = 0; i < sizeof(kContractIDs) / sizeof(kContractIDs[0]); ++i) {
        if (kContractIDs[i] == templateID) return true;
    }
    return false;
}

struct CurrentContractSelection {
    int slot;
    uint32_t templateID;
    uint32_t identity;
};

bool ResolveCurrentPremiumContractSelection(CurrentContractSelection* output)
{
    if (!output || !g_dnfBase) return false;
    output->slot = -1;
    output->templateID = 0;
    output->identity = 0;

    struct NativeIntVector {
        int* begin;
        int* end;
        int* capacity;
    } selected = {};
    bool constructed = false;
    void* manager = nullptr;
    int slot = -1;
    __try {
        manager = *reinterpret_cast<void**>(
            g_dnfBase + kInventorySelectionManagerRva);
        if (!manager) return false;
        InventorySelectionCtorFn ctor = reinterpret_cast<InventorySelectionCtorFn>(
            g_dnfBase + kInventorySelectionCtorRva);
        InventorySelectionCollectFn collect =
            reinterpret_cast<InventorySelectionCollectFn>(
                g_dnfBase + kInventorySelectionCollectRva);
        InventorySelectionDtorFn dtor = reinterpret_cast<InventorySelectionDtorFn>(
            g_dnfBase + kInventorySelectionDtorRva);
        ctor(&selected);
        constructed = true;
        collect(manager, &selected, kMainInventorySelectionGroup);
        if (selected.begin && selected.end && selected.end > selected.begin) {
            slot = *selected.begin;
        }
        dtor(&selected);
        constructed = false;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        if (constructed) {
            __try {
                InventorySelectionDtorFn dtor =
                    reinterpret_cast<InventorySelectionDtorFn>(
                        g_dnfBase + kInventorySelectionDtorRva);
                dtor(&selected);
            }
            __except (EXCEPTION_EXECUTE_HANDLER) {
            }
        }
        LogLine("contract-use selection read exception code=0x%08X",
            GetExceptionCode());
        return false;
    }

    uint32_t templateID = 0;
    uint32_t identity = 0;
    __try {
        InventorySelectionLookupFn lookup =
            reinterpret_cast<InventorySelectionLookupFn>(
                g_dnfBase + kInventorySelectionLookupRva);
        void* selectedItem = lookup(manager, slot);
        if (!selectedItem) return false;
        void** vtable = *reinterpret_cast<void***>(selectedItem);
        if (!vtable) return false;
        CurrentItemDataFn itemDataFn =
            reinterpret_cast<CurrentItemDataFn>(vtable[0x28 / sizeof(void*)]);
        if (!itemDataFn) return false;
        void* itemData = itemDataFn(selectedItem);
        if (!itemData) return false;

        // This is the exact current-EXE read sequence used immediately before
        // both native op44 writers.  Reading the selected wrapper avoids
        // confusing same-numbered slots from the equipment, material, avatar,
        // pet, and consumable lists in the passive parser cache.
        CurrentItemTemplateReadFn readTemplate =
            reinterpret_cast<CurrentItemTemplateReadFn>(
                g_dnfBase + kCurrentItemTemplateReadRva);
        templateID = readTemplate(
            reinterpret_cast<unsigned char*>(itemData) + 0x14);
        if (!IsCurrentPremiumContractID(templateID)) return false;

        CurrentItemIdentityReadFn readIdentity =
            reinterpret_cast<CurrentItemIdentityReadFn>(
                g_dnfBase + kCurrentItemIdentityReadRva);
        identity = readIdentity(templateID);
        if (identity == 0) return false;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("contract-use selected-item read exception slot=%d code=0x%08X",
            slot, GetExceptionCode());
        return false;
    }

    output->slot = slot;
    output->templateID = templateID;
    output->identity = identity;
    return true;
}

// sub_2331CE0 writes its own item and slot arguments into op44 and obtains
// the same instance identity through sub_2064A20.  Resolve the fallback from
// those native arguments rather than the generic selection manager: the
// active-coupon panel does not need to keep the latter's UI selection alive.
bool ResolveCurrentPremiumContractPanelSelection(
    uint32_t templateID, int slot, CurrentContractSelection* output)
{
    if (!output) return false;
    output->slot = -1;
    output->templateID = 0;
    output->identity = 0;
    if (!g_dnfBase || slot < 0 || slot > 0x7FFF ||
        !IsCurrentPremiumContractID(templateID)) {
        return false;
    }

    uint32_t identity = 0;
    __try {
        CurrentItemIdentityReadFn readIdentity =
            reinterpret_cast<CurrentItemIdentityReadFn>(
                g_dnfBase + kCurrentItemIdentityReadRva);
        identity = readIdentity(templateID);
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("contract-use panel item-identity read exception slot=%d item=%lu code=0x%08X",
            slot, static_cast<unsigned long>(templateID), GetExceptionCode());
        return false;
    }
    if (identity == 0) return false;

    output->slot = slot;
    output->templateID = templateID;
    output->identity = identity;
    return true;
}

bool SendCurrentPremiumContractUse(const CurrentContractSelection& selection)
{
    if (selection.slot < 0 || selection.slot > 0x7FFF ||
        !IsCurrentPremiumContractID(selection.templateID) ||
        selection.identity == 0 || !g_dnfBase) {
        return false;
    }
    __try {
        UpperWriterValueFn value = reinterpret_cast<UpperWriterValueFn>(
            g_dnfBase + kUpperWriterValueRva);
        UpperWriterScalarFn writeU16 = reinterpret_cast<UpperWriterScalarFn>(
            g_dnfBase + kUpperWriterU16Rva);
        UpperWriterScalarFn writeU8 = reinterpret_cast<UpperWriterScalarFn>(
            g_dnfBase + kUpperWriterU8Rva);
        UpperWriterScalarFn writeI16 = reinterpret_cast<UpperWriterScalarFn>(
            g_dnfBase + kUpperWriterI16Rva);
        UpperWriterScalarFn writeU32 = reinterpret_cast<UpperWriterScalarFn>(
            g_dnfBase + kUpperWriterU32Rva);
        UpperWriterFlushFn flush = reinterpret_cast<UpperWriterFlushFn>(
            g_dnfBase + kUpperWriterFlushRva);

        writeU16(value(), kUseStackableOpcode);
        writeI16(value(), static_cast<uint16_t>(selection.slot));
        writeU8(value(), kMainInventoryListType);
        writeU32(value(), selection.identity);
        writeU32(value(), selection.templateID);
        writeU32(value(), 0);
        flush();
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("contract-use fallback send exception slot=%d item=%lu identity=%lu code=0x%08X",
            selection.slot, static_cast<unsigned long>(selection.templateID),
            static_cast<unsigned long>(selection.identity), GetExceptionCode());
        return false;
    }
    LogLine("contract-use fallback sent opcode=44 slot=%d list=0 item=%lu identity=%lu",
        selection.slot, static_cast<unsigned long>(selection.templateID),
        static_cast<unsigned long>(selection.identity));
    return true;
}

LONG FindCurrentItemRowsByTemplate(uint32_t templateID, CurrentItemRowTrace* output, LONG outputCapacity)
{
    if (templateID == 0 || !output || outputCapacity <= 0) return 0;
    LONG found = 0;
    AcquireSRWLockShared(&g_socketTraceLock);
    for (LONG i = 0; i < kSocketTraceCacheRows; ++i) {
        const CurrentItemRowTrace& row = g_socketTraceRows[i];
        if (row.sequence == 0 || CurrentItemRowTemplateID(row.raw) != templateID) continue;
        memcpy(&output[found], &row, sizeof(row));
        ++found;
        if (found == outputCapacity) break;
    }
    ReleaseSRWLockShared(&g_socketTraceLock);
    return found;
}

bool ResolveHoveredPremiumContractSelection(CurrentContractSelection* output)
{
    if (!output || !g_dnfBase) return false;
    output->slot = -1;
    output->templateID = 0;
    output->identity = 0;

    void* hovered = nullptr;
    void* hoveredItemData = nullptr;
    uint32_t templateID = 0;
    __try {
        hovered = *reinterpret_cast<void**>(
            g_dnfBase + kHoveredInventoryItemRva);
        if (!hovered) return false;
        void** vtable = *reinterpret_cast<void***>(hovered);
        if (!vtable) return false;
        CurrentItemDataFn itemDataFn =
            reinterpret_cast<CurrentItemDataFn>(
                vtable[0x28 / sizeof(void*)]);
        if (!itemDataFn) return false;
        void* itemData = itemDataFn(hovered);
        if (!itemData) return false;
        hoveredItemData = itemData;
        CurrentItemTemplateReadFn readTemplate =
            reinterpret_cast<CurrentItemTemplateReadFn>(
                g_dnfBase + kCurrentItemTemplateReadRva);
        templateID = readTemplate(
            reinterpret_cast<unsigned char*>(itemData) + 0x14);
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("contract-use hovered-item read exception code=0x%08X",
            GetExceptionCode());
        return false;
    }
    if (!IsCurrentPremiumContractID(templateID)) return false;

    // The native selection group is authoritative when the inventory UI has
    // populated it.  Some current item classes reject right-click dispatch
    // before that group is committed, so the passive op14 row cache provides
    // the exact slot as the bounded fallback.
    CurrentContractSelection selected = {};
    if (ResolveCurrentPremiumContractSelection(&selected) &&
        selected.templateID == templateID) {
        *output = selected;
        return true;
    }

    // The passive row parser also observes warehouse and other container
    // rows, which can carry the same template and slot number as list 0.
    // Resolve the hovered template through the current main-inventory manager
    // instead.  Prefer the exact wrapper/item-data object under the mouse; a
    // single same-template main-bag row is the bounded fallback.
    int exactSlot = -1;
    int uniqueSlot = -1;
    int nativeMatchCount = 0;
    __try {
        void* manager = *reinterpret_cast<void**>(
            g_dnfBase + kInventorySelectionManagerRva);
        if (!manager) return false;
        InventorySelectionLookupFn lookup =
            reinterpret_cast<InventorySelectionLookupFn>(
                g_dnfBase + kInventorySelectionLookupRva);
        CurrentItemTemplateReadFn readTemplate =
            reinterpret_cast<CurrentItemTemplateReadFn>(
                g_dnfBase + kCurrentItemTemplateReadRva);
        for (int slot = kCurrentMainInventoryFirstItemSlot;
             slot <= kCurrentMainInventoryLastItemSlot; ++slot) {
            void* candidate = lookup(manager, slot);
            if (!candidate) continue;
            void** vtable = *reinterpret_cast<void***>(candidate);
            if (!vtable) continue;
            CurrentItemDataFn itemDataFn =
                reinterpret_cast<CurrentItemDataFn>(
                    vtable[0x28 / sizeof(void*)]);
            if (!itemDataFn) continue;
            void* candidateItemData = itemDataFn(candidate);
            if (!candidateItemData) continue;
            uint32_t candidateTemplateID = readTemplate(
                reinterpret_cast<unsigned char*>(candidateItemData) + 0x14);
            if (candidateTemplateID != templateID) continue;
            ++nativeMatchCount;
            uniqueSlot = slot;
            if (candidate == hovered || candidateItemData == hoveredItemData) {
                exactSlot = slot;
                break;
            }
        }
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("contract-use main-inventory scan exception item=%lu code=0x%08X",
            static_cast<unsigned long>(templateID), GetExceptionCode());
        return false;
    }
    int slot = exactSlot >= 0
        ? exactSlot
        : (nativeMatchCount == 1 ? uniqueSlot : -1);
    if (slot < 0) {
        LogLine("contract-use main-inventory resolve rejected item=%lu native_matches=%d exact_slot=%d",
            static_cast<unsigned long>(templateID), nativeMatchCount, exactSlot);
        return false;
    }

    uint32_t identity = 0;
    __try {
        CurrentItemIdentityReadFn readIdentity =
            reinterpret_cast<CurrentItemIdentityReadFn>(
                g_dnfBase + kCurrentItemIdentityReadRva);
        identity = readIdentity(templateID);
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("contract-use hovered identity read exception item=%lu code=0x%08X",
            static_cast<unsigned long>(templateID), GetExceptionCode());
        return false;
    }
    if (identity == 0) return false;

    output->slot = slot;
    output->templateID = templateID;
    output->identity = identity;
    LogLine("contract-use main-inventory resolved slot=%d item=%lu native_matches=%d exact_hover=%d",
        slot, static_cast<unsigned long>(templateID), nativeMatchCount,
        exactSlot >= 0 ? 1 : 0);
    return true;
}

bool RejectLuaClientNotice(const char* reason)
{
    LONG hit = InterlockedIncrement(&g_luaNoticeRejectedCount);
    if (hit <= 8) {
        LogLine("lua client notice rejected hit=%ld reason=%s", hit, reason);
    }
    return false;
}

const unsigned char* LoadEmbeddedPixelResource(
    int resourceID, size_t expectedBytes)
{
    HMODULE module = GetModuleHandleW(L"90CN.dll");
    if (!module) return nullptr;
    HRSRC resource = FindResourceW(
        module, MAKEINTRESOURCEW(resourceID), RT_RCDATA);
    if (!resource || SizeofResource(module, resource) != expectedBytes) {
        return nullptr;
    }
    HGLOBAL loaded = LoadResource(module, resource);
    return loaded
        ? reinterpret_cast<const unsigned char*>(LockResource(loaded))
        : nullptr;
}

void BlendPremultipliedPixels(
    unsigned char* destination, int destinationWidth,
    int destinationHeight, int destinationX, int destinationY,
    const unsigned char* source, int sourceWidth, int sourceHeight,
    int copyWidth, int copyHeight)
{
    if (!destination || !source || destinationWidth <= 0 ||
        destinationHeight <= 0 || sourceWidth <= 0 || sourceHeight <= 0 ||
        copyWidth <= 0 || copyHeight <= 0 || destinationX < 0 ||
        destinationY < 0 || destinationX + copyWidth > destinationWidth ||
        destinationY + copyHeight > destinationHeight ||
        copyWidth > sourceWidth || copyHeight > sourceHeight) {
        return;
    }

    for (int y = 0; y < copyHeight; ++y) {
        unsigned char* destinationRow = destination +
            ((destinationY + y) * destinationWidth + destinationX) * 4;
        const unsigned char* sourceRow = source + y * sourceWidth * 4;
        for (int x = 0; x < copyWidth; ++x) {
            unsigned char* destinationPixel = destinationRow + x * 4;
            const unsigned char* sourcePixel = sourceRow + x * 4;
            const unsigned int sourceAlpha = sourcePixel[3];
            const unsigned int inverseAlpha = 255 - sourceAlpha;
            for (int channel = 0; channel < 3; ++channel) {
                unsigned int value = sourcePixel[channel] +
                    (destinationPixel[channel] * inverseAlpha + 127) / 255;
                destinationPixel[channel] = static_cast<unsigned char>(
                    value > 255 ? 255 : value);
            }
            unsigned int alpha = sourceAlpha +
                (destinationPixel[3] * inverseAlpha + 127) / 255;
            destinationPixel[3] = static_cast<unsigned char>(
                alpha > 255 ? 255 : alpha);
        }
    }
}

LRESULT CALLBACK LuaNoticeOverlayWindowProc(
    HWND window, UINT message, WPARAM wParam, LPARAM lParam)
{
    switch (message) {
    case WM_ERASEBKGND:
        return 1;
    case WM_NCHITTEST:
        return HTTRANSPARENT;
    case WM_MOUSEACTIVATE:
        return MA_NOACTIVATE;
    case WM_TIMER:
        if (wParam == kLuaNoticeOverlayTimer) {
            KillTimer(window, kLuaNoticeOverlayTimer);
            ShowWindow(window, SW_HIDE);
            return 0;
        }
        break;
    case WM_PAINT: {
        PAINTSTRUCT paint = {};
        HDC dc = BeginPaint(window, &paint);
        RECT client = {};
        GetClientRect(window, &client);

        const COLORREF colorKey = RGB(1, 1, 1);
        HBRUSH clearBrush = CreateSolidBrush(colorKey);
        FillRect(dc, &client, clearBrush);
        DeleteObject(clearBrush);

        HBRUSH panelBrush = CreateSolidBrush(RGB(24, 28, 36));
        HPEN borderPen = CreatePen(PS_SOLID, 2, RGB(230, 184, 74));
        HGDIOBJ oldBrush = SelectObject(dc, panelBrush);
        HGDIOBJ oldPen = SelectObject(dc, borderPen);
        RoundRect(dc, client.left + 2, client.top + 2,
            client.right - 2, client.bottom - 2, 18, 18);
        SelectObject(dc, oldPen);
        SelectObject(dc, oldBrush);
        DeleteObject(borderPen);
        DeleteObject(panelBrush);

        HFONT font = CreateFontW(-24, 0, 0, 0, FW_BOLD, FALSE, FALSE, FALSE,
            DEFAULT_CHARSET, OUT_DEFAULT_PRECIS, CLIP_DEFAULT_PRECIS,
            CLEARTYPE_QUALITY, DEFAULT_PITCH | FF_DONTCARE,
            L"Microsoft YaHei");
        bool ownsFont = font != nullptr;
        if (!font) {
            font = reinterpret_cast<HFONT>(GetStockObject(DEFAULT_GUI_FONT));
        }
        HGDIOBJ oldFont = SelectObject(dc, font);
        SetBkMode(dc, TRANSPARENT);
        SetTextColor(dc, RGB(255, 248, 224));
        RECT textRect = client;
        textRect.left += 18;
        textRect.right -= 18;
        DrawTextW(dc, g_luaNoticeOverlayText, -1, &textRect,
            DT_CENTER | DT_VCENTER | DT_SINGLELINE | DT_END_ELLIPSIS |
            DT_NOPREFIX);
        SelectObject(dc, oldFont);
        if (ownsFont) DeleteObject(font);
        EndPaint(window, &paint);
        return 0;
    }
    case WM_NCDESTROY:
        if (g_luaNoticeOverlayWindow == window) {
            g_luaNoticeOverlayWindow = nullptr;
        }
        break;
    default:
        break;
    }
    return DefWindowProcW(window, message, wParam, lParam);
}

bool EnsureLuaNoticeOverlayWindow(HWND parent)
{
    if (g_luaNoticeOverlayWindow &&
        IsWindow(g_luaNoticeOverlayWindow)) {
        return true;
    }

    HINSTANCE module = reinterpret_cast<HINSTANCE>(
        GetModuleHandleW(L"90CN.dll"));
    WNDCLASSEXW windowClass = {};
    windowClass.cbSize = sizeof(windowClass);
    windowClass.style = CS_HREDRAW | CS_VREDRAW;
    windowClass.lpfnWndProc = &LuaNoticeOverlayWindowProc;
    windowClass.hInstance = module;
    windowClass.lpszClassName = kLuaNoticeOverlayClassName;
    if (!RegisterClassExW(&windowClass) &&
        GetLastError() != ERROR_CLASS_ALREADY_EXISTS) {
        LogLine("lua client notice overlay class registration failed error=%lu",
            GetLastError());
        return false;
    }

    HWND overlay = CreateWindowExW(
        WS_EX_LAYERED | WS_EX_TRANSPARENT | WS_EX_NOACTIVATE |
            WS_EX_TOOLWINDOW,
        kLuaNoticeOverlayClassName, L"", WS_POPUP,
        0, 0, 0, 0, parent, nullptr, module, nullptr);
    if (!overlay) {
        LogLine("lua client notice overlay creation failed error=%lu",
            GetLastError());
        return false;
    }
    if (!SetLayeredWindowAttributes(
            overlay, RGB(1, 1, 1), 238, LWA_COLORKEY | LWA_ALPHA)) {
        LogLine("lua client notice overlay alpha setup failed error=%lu",
            GetLastError());
        DestroyWindow(overlay);
        return false;
    }
    g_luaNoticeOverlayWindow = overlay;
    return true;
}

bool ShowLuaClientNoticeOverlay(
    HWND parent, const LuaClientNotice& notice)
{
    if (!EnsureLuaNoticeOverlayWindow(parent)) return false;

    memcpy_s(g_luaNoticeOverlayText, sizeof(g_luaNoticeOverlayText),
        notice.text, (notice.length + 1) * sizeof(wchar_t));

    RECT parentClient = {};
    if (!GetClientRect(parent, &parentClient)) return false;
    int parentWidth = parentClient.right - parentClient.left;
    int parentHeight = parentClient.bottom - parentClient.top;
    int width = parentWidth - 40;
    if (width > 900) width = 900;
    if (width < 280) width = 280;
    int height = 58;
    int x = (parentWidth - width) / 2;
    if (x < 0) x = 0;
    int y = parentHeight >= 400 ? 70 : 20;
    POINT screenPosition = { x, y };
    if (!ClientToScreen(parent, &screenPosition)) return false;

    InvalidateRect(g_luaNoticeOverlayWindow, nullptr, TRUE);
    SetWindowPos(g_luaNoticeOverlayWindow, parent,
        screenPosition.x, screenPosition.y, width, height,
        SWP_NOACTIVATE | SWP_SHOWWINDOW);
    UpdateWindow(g_luaNoticeOverlayWindow);
    SetTimer(g_luaNoticeOverlayWindow, kLuaNoticeOverlayTimer,
        kLuaNoticeOverlayDurationMs, nullptr);
    return true;
}

const wchar_t* CombatPowerRankName(unsigned int score)
{
    if (score < 3100) return L"探索";
    if (score < 8100) return L"开拓";
    if (score < 23000) return L"无畏";
    if (score < 52000) return L"征服";
    if (score < 65000) return L"战绝";
    if (score < 130000) return L"英杰";
    return L"武炼";
}

bool TryComputeCombatPowerEquipmentBonusHundredths(
    const DNF90CombatPanelState& state, unsigned int* output)
{
    if (!output || state.baseAttributeScore == 0 ||
        (state.validFlags & DNF90_COMBAT_PANEL_BASE_SCORE_VALID) == 0 ||
        (state.validFlags & DNF90_COMBAT_PANEL_EQUIPMENT_SCORE_VALID) == 0 ||
        (state.validFlags & DNF90_COMBAT_PANEL_DAMAGE_AFFIXES_VALID) == 0) {
        return false;
    }

    // Lua's equipment score contains both the three visible damage-affix
    // multiplier score and the remaining equipment-derived score. Remove the
    // white/yellow/critical part first so the "equipment" percentage is an
    // independent row and never counts the displayed affixes twice.
    const unsigned int yellowTenths = state.yellowDamageTenths +
        state.yellowAdditionalTenths;
    const unsigned int criticalTenths = state.criticalDamageTenths +
        state.criticalAdditionalTenths;
    const long double multiplier =
        (1.0L + static_cast<long double>(state.whiteDamageTenths) / 1000.0L) *
        (1.0L + static_cast<long double>(yellowTenths) / 1000.0L) *
        (1.0L + static_cast<long double>(criticalTenths) / 1000.0L);
    const long double affixValue =
        static_cast<long double>(state.baseAttributeScore) *
        (multiplier - 1.0L);
    unsigned int affixScore = 0;
    if (affixValue >= static_cast<long double>(state.equipmentScore)) {
        affixScore = state.equipmentScore;
    } else if (affixValue > 0.0L) {
        affixScore = static_cast<unsigned int>(affixValue);
    }
    const unsigned int equipmentOnlyScore =
        state.equipmentScore - affixScore;
    const unsigned long long scaled =
        static_cast<unsigned long long>(equipmentOnlyScore) * 10000ull;
    const unsigned long long hundredths =
        scaled / state.baseAttributeScore;
    if (hundredths > 999999ull) return false;
    *output = static_cast<unsigned int>(hundredths);
    return true;
}

int CombatPowerRankIconResource(unsigned int score)
{
    if (score < 3100) return IDR_COMBAT_RANK_EXPLORE_V3;
    if (score < 8100) return IDR_COMBAT_RANK_PIONEER_V3;
    if (score < 23000) return IDR_COMBAT_RANK_FEARLESS_V3;
    if (score < 52000) return IDR_COMBAT_RANK_CONQUER_V3;
    if (score < 65000) return IDR_COMBAT_RANK_BATTLE_PINNACLE_V3;
    if (score < 130000) return IDR_COMBAT_RANK_HEROIC_V3;
    return IDR_COMBAT_RANK_MARTIAL_MASTERY_V3;
}

HFONT CreateCombatPanelFont(int pixelHeight, int weight)
{
    HFONT font = CreateFontW(-pixelHeight, 0, 0, 0, weight,
        FALSE, FALSE, FALSE, DEFAULT_CHARSET, OUT_DEFAULT_PRECIS,
        CLIP_DEFAULT_PRECIS, CLEARTYPE_QUALITY,
        DEFAULT_PITCH | FF_DONTCARE, L"Microsoft YaHei");
    return font ? font : reinterpret_cast<HFONT>(
        GetStockObject(DEFAULT_GUI_FONT));
}

void DrawCombatPanelText(HDC dc, const wchar_t* text, RECT bounds,
    int pixelHeight, int weight, COLORREF color, UINT format)
{
    HFONT font = CreateCombatPanelFont(pixelHeight, weight);
    HGDIOBJ oldFont = SelectObject(dc, font);
    SetBkMode(dc, TRANSPARENT);
    SetTextColor(dc, color);
    DrawTextW(dc, text, -1, &bounds, format | DT_NOPREFIX);
    SelectObject(dc, oldFont);
    if (font != GetStockObject(DEFAULT_GUI_FONT)) DeleteObject(font);
}

void DrawCombatPanelTextShadowed(HDC dc, const wchar_t* text, RECT bounds,
    int pixelHeight, int weight, COLORREF color, UINT format)
{
    RECT shadow = bounds;
    OffsetRect(&shadow, 1, 1);
    DrawCombatPanelText(dc, text, shadow, pixelHeight, weight,
        RGB(0, 0, 0), format);
    DrawCombatPanelText(dc, text, bounds, pixelHeight, weight, color, format);
}

bool DrawEmbeddedBgraResource(HDC dc, int resourceID, int width, int height)
{
    const size_t expectedBytes = static_cast<size_t>(width) * height * 4;
    const unsigned char* pixels = LoadEmbeddedPixelResource(
        resourceID, expectedBytes);
    if (!pixels) return false;

    BITMAPINFO bitmapInfo = {};
    bitmapInfo.bmiHeader.biSize = sizeof(bitmapInfo.bmiHeader);
    bitmapInfo.bmiHeader.biWidth = width;
    bitmapInfo.bmiHeader.biHeight = -height;
    bitmapInfo.bmiHeader.biPlanes = 1;
    bitmapInfo.bmiHeader.biBitCount = 32;
    bitmapInfo.bmiHeader.biCompression = BI_RGB;
    return StretchDIBits(dc,
        0, 0, width, height,
        0, 0, width, height,
        pixels, &bitmapInfo, DIB_RGB_COLORS, SRCCOPY) != GDI_ERROR;
}

bool DrawCombatPanelSkinWithRankIcon(HDC dc, unsigned int score)
{
    const size_t panelBytes = static_cast<size_t>(kCombatPanelWidth) *
        kCombatPanelHeight * 4;
    const size_t iconBytes = static_cast<size_t>(kCombatRankIconSize) *
        kCombatRankIconSize * 4;
    const unsigned char* panel = LoadEmbeddedPixelResource(
        IDR_COMBAT_POWER_PANEL_SKIN_V2, panelBytes);
    const unsigned char* icon = LoadEmbeddedPixelResource(
        CombatPowerRankIconResource(score), iconBytes);
    if (!panel || !icon) return false;

    unsigned char* composed = static_cast<unsigned char*>(
        HeapAlloc(GetProcessHeap(), 0, panelBytes));
    if (!composed) return false;
    memcpy(composed, panel, panelBytes);
    BlendPremultipliedPixels(
        composed, kCombatPanelWidth, kCombatPanelHeight,
        kCombatRankIconX, kCombatRankIconY,
        icon, kCombatRankIconSize, kCombatRankIconSize,
        kCombatRankIconSize, kCombatRankIconSize);

    BITMAPINFO bitmapInfo = {};
    bitmapInfo.bmiHeader.biSize = sizeof(bitmapInfo.bmiHeader);
    bitmapInfo.bmiHeader.biWidth = kCombatPanelWidth;
    bitmapInfo.bmiHeader.biHeight = -kCombatPanelHeight;
    bitmapInfo.bmiHeader.biPlanes = 1;
    bitmapInfo.bmiHeader.biBitCount = 32;
    bitmapInfo.bmiHeader.biCompression = BI_RGB;
    const bool drawn = StretchDIBits(dc,
        0, 0, kCombatPanelWidth, kCombatPanelHeight,
        0, 0, kCombatPanelWidth, kCombatPanelHeight,
        composed, &bitmapInfo, DIB_RGB_COLORS, SRCCOPY) != GDI_ERROR;
    HeapFree(GetProcessHeap(), 0, composed);
    return drawn;
}

void DrawCombatPanelProgressBar(HDC dc, int left, int top, int width,
    unsigned int numerator, unsigned int denominator, COLORREF fillColor)
{
    if (width <= 2) return;
    HBRUSH track = CreateSolidBrush(RGB(3, 11, 19));
    RECT trackRect = { left, top, left + width, top + 6 };
    FillRect(dc, &trackRect, track);
    DeleteObject(track);

    HPEN border = CreatePen(PS_SOLID, 1, RGB(47, 84, 105));
    HGDIOBJ oldPen = SelectObject(dc, border);
    HGDIOBJ oldBrush = SelectObject(dc, GetStockObject(NULL_BRUSH));
    Rectangle(dc, trackRect.left, trackRect.top,
        trackRect.right, trackRect.bottom);
    SelectObject(dc, oldBrush);
    SelectObject(dc, oldPen);
    DeleteObject(border);

    if (denominator == 0 || numerator == 0) return;
    unsigned int fill = static_cast<unsigned int>(
        (static_cast<unsigned long long>(width - 2) * numerator) /
        denominator);
    if (fill > static_cast<unsigned int>(width - 2)) fill = width - 2;
    HBRUSH value = CreateSolidBrush(fillColor);
    RECT valueRect = { left + 1, top + 1,
        left + 1 + static_cast<int>(fill), top + 5 };
    FillRect(dc, &valueRect, value);
    DeleteObject(value);
}

#if 0
void DrawCombatPanelMainLegacy(HDC dc, const DNF90CombatPanelState& state)
{
    const bool baseValid =
        (state.validFlags & DNF90_COMBAT_PANEL_BASE_SCORE_VALID) != 0;
    const bool affixesValid =
        (state.validFlags & DNF90_COMBAT_PANEL_DAMAGE_AFFIXES_VALID) != 0;
    const bool skinDrawn = baseValid
        ? DrawCombatPanelSkinWithRankIcon(dc, state.totalScore)
        : DrawEmbeddedBgraResource(dc, IDR_COMBAT_POWER_PANEL_SKIN_V2,
            kCombatPanelWidth, kCombatPanelHeight);
    if (!skinDrawn) {
        RECT fallback = { 0, 0, kCombatPanelWidth, kCombatPanelHeight };
        HBRUSH background = CreateSolidBrush(RGB(5, 17, 28));
        FillRect(dc, &fallback, background);
        DeleteObject(background);
    }

    wchar_t line[64] = {};

    RECT title = { 7, 5, 113, 28 };
    DrawCombatPanelTextShadowed(dc, L"我的战斗力值", title, 13, FW_BOLD,
        RGB(225, 242, 251), DT_CENTER | DT_VCENTER | DT_SINGLELINE);
    RECT help = { 105, 25, 122, 42 };
    DrawCombatPanelTextShadowed(dc, L"?", help, 11, FW_BOLD,
        RGB(245, 202, 91), DT_CENTER | DT_VCENTER | DT_SINGLELINE);

    if (!baseValid) {
        RECT medalText = { 37, 43, 91, 101 };
        DrawCombatPanelTextShadowed(dc, L"战", medalText, 27, FW_BOLD,
            RGB(232, 247, 255), DT_CENTER | DT_VCENTER | DT_SINGLELINE);
    }
    if (baseValid) {
        swprintf_s(line, L"%u", state.totalScore);
    } else {
        wcscpy_s(line, L"--");
    }
    RECT total = { 8, 116, 120, 143 };
    DrawCombatPanelTextShadowed(dc, line, total, 19, FW_BOLD,
        RGB(255, 225, 101), DT_CENTER | DT_VCENTER | DT_SINGLELINE);

    const wchar_t* rank = baseValid
        ? CombatPowerRankName(state.totalScore)
        : L"--";
    // The selected TGP badge has a dedicated empty nameplate below the
    // emblem.  Keep the rank inside that plate instead of spending a separate
    // row below the total score.
    RECT rankRect = { 34, 96, 94, 114 };
    DrawCombatPanelTextShadowed(dc, rank, rankRect, 9, FW_BOLD,
        RGB(245, 226, 171), DT_CENTER | DT_VCENTER | DT_SINGLELINE);

    RECT baseLabel = { 9, 158, 119, 176 };
    DrawCombatPanelTextShadowed(dc, L"基础属性加成", baseLabel, 10, FW_BOLD,
        RGB(184, 224, 242), DT_CENTER | DT_VCENTER | DT_SINGLELINE);
    if (baseValid) swprintf_s(line, L"%u", state.baseAttributeScore);
    else wcscpy_s(line, L"--");
    RECT baseScore = { 9, 174, 119, 195 };
    DrawCombatPanelTextShadowed(dc, line, baseScore, 12, FW_BOLD,
        RGB(103, 218, 255), DT_CENTER | DT_VCENTER | DT_SINGLELINE);

    RECT equipmentLabel = { 9, 198, 119, 225 };
    DrawCombatPanelTextShadowed(dc, L"装备加成", equipmentLabel, 10,
        FW_BOLD, RGB(184, 224, 242),
        DT_CENTER | DT_VCENTER | DT_SINGLELINE);

    RECT affix = { 12, 227, 116, 252 };
    if (affixesValid) {
        swprintf_s(line, L"白字  %u.%u0%%",
            state.whiteDamageTenths / 10,
            state.whiteDamageTenths % 10);
        DrawCombatPanelTextShadowed(dc, line, affix, 10, FW_NORMAL,
            RGB(225, 236, 242), DT_CENTER | DT_VCENTER | DT_SINGLELINE);
        swprintf_s(line, L"黄字  %u.%u0%%",
            state.yellowDamageTenths / 10,
            state.yellowDamageTenths % 10);
        affix = { 12, 252, 116, 277 };
        DrawCombatPanelTextShadowed(dc, line, affix, 10, FW_NORMAL,
            RGB(255, 216, 95), DT_CENTER | DT_VCENTER | DT_SINGLELINE);
        swprintf_s(line, L"爆伤  %u.%u0%%",
            state.criticalDamageTenths / 10,
            state.criticalDamageTenths % 10);
        affix = { 12, 277, 116, 302 };
        DrawCombatPanelTextShadowed(dc, line, affix, 10, FW_NORMAL,
            RGB(255, 174, 104), DT_CENTER | DT_VCENTER | DT_SINGLELINE);
        swprintf_s(line, L"黄追  %u.%u0%%",
            state.yellowAdditionalTenths / 10,
            state.yellowAdditionalTenths % 10);
        affix = { 12, 302, 116, 327 };
        DrawCombatPanelTextShadowed(dc, line, affix, 10, FW_NORMAL,
            RGB(249, 232, 145), DT_CENTER | DT_VCENTER | DT_SINGLELINE);
        swprintf_s(line, L"爆追  %u.%u0%%",
            state.criticalAdditionalTenths / 10,
            state.criticalAdditionalTenths % 10);
        affix = { 12, 327, 116, 352 };
        DrawCombatPanelTextShadowed(dc, line, affix, 10, FW_NORMAL,
            RGB(246, 190, 132), DT_CENTER | DT_VCENTER | DT_SINGLELINE);
        swprintf_s(line, L"全攻  %u.%u0%%",
            state.allAttackTenths / 10,
            state.allAttackTenths % 10);
        affix = { 12, 352, 116, 377 };
        DrawCombatPanelTextShadowed(dc, line, affix, 10, FW_NORMAL,
            RGB(161, 216, 241), DT_CENTER | DT_VCENTER | DT_SINGLELINE);
    } else {
        const wchar_t* pendingRows[] = {
            L"白字  --", L"黄字  --", L"爆伤  --",
            L"黄追  --", L"爆追  --", L"全攻  --",
        };
        for (size_t index = 0;
             index < sizeof(pendingRows) / sizeof(pendingRows[0]); ++index) {
            affix.top = 227 + static_cast<LONG>(index) * 25;
            affix.bottom = affix.top + 25;
            DrawCombatPanelTextShadowed(dc, pendingRows[index], affix, 10,
                FW_NORMAL, RGB(143, 193, 215),
                DT_CENTER | DT_VCENTER | DT_SINGLELINE);
        }
    }
}

#endif

void DrawCombatPanelMain(HDC dc, const DNF90CombatPanelState& state)
{
    const bool baseValid =
        (state.validFlags & DNF90_COMBAT_PANEL_BASE_SCORE_VALID) != 0;
    const bool affixesValid =
        (state.validFlags & DNF90_COMBAT_PANEL_DAMAGE_AFFIXES_VALID) != 0;
    const bool skinDrawn = baseValid
        ? DrawCombatPanelSkinWithRankIcon(dc, state.totalScore)
        : DrawEmbeddedBgraResource(dc, IDR_COMBAT_POWER_PANEL_SKIN_V2,
            kCombatPanelWidth, kCombatPanelHeight);
    if (!skinDrawn) {
        RECT fallback = { 0, 0, kCombatPanelWidth, kCombatPanelHeight };
        HBRUSH background = CreateSolidBrush(RGB(5, 17, 28));
        FillRect(dc, &fallback, background);
        DeleteObject(background);
    }

    // 标题、问号、分区标题和金属格子来自原版位图；这里只绘制实时数据。
    wchar_t line[64] = {};
    if (baseValid) swprintf_s(line, L"%u", state.totalScore);
    else wcscpy_s(line, L"--");
    RECT total = { 5, 128, 113, 153 };
    DrawCombatPanelTextShadowed(dc, line, total, 18, FW_BOLD,
        RGB(255, 225, 101), DT_CENTER | DT_VCENTER | DT_SINGLELINE);

    const wchar_t* rank = baseValid
        ? CombatPowerRankName(state.totalScore)
        : L"--";
    HBRUSH rankPlate = CreateSolidBrush(RGB(78, 52, 25));
    RECT rankPlateRect = { 35, 102, 83, 117 };
    FillRect(dc, &rankPlateRect, rankPlate);
    DeleteObject(rankPlate);
    // The copper nameplate is visually one pixel right of the panel's
    // mathematical center. Center against the plate itself and use the next
    // font size so two-character ranks fill it like the original TGP panel.
    RECT rankRect = { 32, 100, 88, 120 };
    DrawCombatPanelTextShadowed(dc, rank, rankRect, 10, FW_BOLD,
        RGB(245, 226, 171), DT_CENTER | DT_VCENTER | DT_SINGLELINE);

    wchar_t profession[32] = L"--";
    const bool identityValid =
        (state.validFlags & DNF90_COMBAT_PANEL_IDENTITY_VALID) != 0;
    if (identityValid && state.professionUtf8[0] != '\0') {
        wchar_t converted[32] = {};
        const int convertedCount = MultiByteToWideChar(
            CP_UTF8, MB_ERR_INVALID_CHARS, state.professionUtf8, -1,
            converted, static_cast<int>(_countof(converted)));
        if (convertedCount > 0) wcscpy_s(profession, converted);
    }
    if (identityValid) swprintf_s(line, L"%s  Lv.%u", profession, state.level);
    else wcscpy_s(line, L"--  Lv.--");
    RECT identity = { 5, 153, 113, 177 };
    DrawCombatPanelTextShadowed(dc, line, identity, 10, FW_NORMAL,
        RGB(220, 229, 232), DT_CENTER | DT_VCENTER | DT_SINGLELINE);

    if (baseValid) swprintf_s(line, L"%u", state.baseAttributeScore);
    else wcscpy_s(line, L"--");
    RECT baseScore = { 5, 224, 113, 251 };
    DrawCombatPanelTextShadowed(dc, line, baseScore, 12, FW_BOLD,
        RGB(103, 218, 255), DT_CENTER | DT_VCENTER | DT_SINGLELINE);

    RECT affix = { 7, 274, 111, 302 };
    if (affixesValid) {
        swprintf_s(line, L"白字  %u.%u0%%",
            state.whiteDamageTenths / 10,
            state.whiteDamageTenths % 10);
        DrawCombatPanelTextShadowed(dc, line, affix, 10, FW_NORMAL,
            RGB(225, 236, 242), DT_CENTER | DT_VCENTER | DT_SINGLELINE);

        const unsigned int yellowTenths = state.yellowDamageTenths +
            state.yellowAdditionalTenths;
        swprintf_s(line, L"黄字  %u.%u0%%",
            yellowTenths / 10, yellowTenths % 10);
        affix = { 7, 302, 111, 330 };
        DrawCombatPanelTextShadowed(dc, line, affix, 10, FW_NORMAL,
            RGB(255, 216, 95), DT_CENTER | DT_VCENTER | DT_SINGLELINE);

        unsigned int equipmentBonusHundredths = 0;
        affix = { 7, 330, 111, 358 };
        if (TryComputeCombatPowerEquipmentBonusHundredths(
                state, &equipmentBonusHundredths)) {
            swprintf_s(line, L"装备  %u.%02u%%",
                equipmentBonusHundredths / 100,
                equipmentBonusHundredths % 100);
        } else {
            wcscpy_s(line, L"装备  --");
        }
        DrawCombatPanelTextShadowed(dc, line, affix, 10, FW_NORMAL,
            RGB(255, 174, 104), DT_CENTER | DT_VCENTER | DT_SINGLELINE);

        const unsigned int criticalTenths = state.criticalDamageTenths +
            state.criticalAdditionalTenths;
        swprintf_s(line, L"爆伤  %u.%u0%%",
            criticalTenths / 10, criticalTenths % 10);
        affix = { 7, 358, 111, 386 };
        DrawCombatPanelTextShadowed(dc, line, affix, 10, FW_NORMAL,
            RGB(139, 218, 244), DT_CENTER | DT_VCENTER | DT_SINGLELINE);
    } else {
        const wchar_t* pendingRows[] = {
            L"白字  --", L"黄字  --", L"装备  --", L"爆伤  --",
        };
        for (size_t index = 0;
             index < sizeof(pendingRows) / sizeof(pendingRows[0]); ++index) {
            affix.top = 274 + static_cast<LONG>(index) * 28;
            affix.bottom = affix.top + 28;
            DrawCombatPanelTextShadowed(dc, pendingRows[index], affix, 10,
                FW_NORMAL, RGB(143, 193, 215),
                DT_CENTER | DT_VCENTER | DT_SINGLELINE);
        }
    }
}

void DrawCombatPanelRankTooltip(HDC dc,
    const DNF90CombatPanelState& state)
{
    if (!DrawEmbeddedBgraResource(dc, IDR_COMBAT_POWER_RANK_TOOLTIP_V2,
            kCombatPanelRankTooltipWidth,
            kCombatPanelRankTooltipHeight)) {
        RECT fallback = { 0, 0, kCombatPanelRankTooltipWidth,
            kCombatPanelRankTooltipHeight };
        HBRUSH background = CreateSolidBrush(RGB(5, 17, 28));
        FillRect(dc, &fallback, background);
        DeleteObject(background);
    }

    RECT rangeHeader = { 19, 17, 112, 43 };
    RECT rankHeader = { 116, 17, 212, 43 };
    DrawCombatPanelTextShadowed(dc, L"战斗力区间", rangeHeader, 12,
        FW_BOLD, RGB(211, 229, 239),
        DT_CENTER | DT_VCENTER | DT_SINGLELINE);
    DrawCombatPanelTextShadowed(dc, L"段位等级", rankHeader, 12,
        FW_BOLD, RGB(211, 229, 239),
        DT_CENTER | DT_VCENTER | DT_SINGLELINE);

    struct RankRow {
        unsigned int lower;
        unsigned int upper;
        const wchar_t* range;
        const wchar_t* name;
    };
    const RankRow rows[] = {
        { 0, 3099, L"0 - 3100", L"探索" },
        { 3100, 8099, L"3100 - 8100", L"开拓" },
        { 8100, 22999, L"8100 - 23000", L"无畏" },
        { 23000, 51999, L"23000 - 52000", L"征服" },
        { 52000, 64999, L"52000 - 65000", L"战绝" },
        { 65000, 129999, L"65000 - 130000", L"英杰" },
        { 130000, 0xFFFFFFFFu, L"130000 以上", L"武炼" },
    };
    for (size_t index = 0; index < sizeof(rows) / sizeof(rows[0]); ++index) {
        const int top = 43 + static_cast<int>(index) * 29;
        const int bottom = top + 29;
        if ((state.validFlags & DNF90_COMBAT_PANEL_BASE_SCORE_VALID) != 0 &&
            state.totalScore >= rows[index].lower &&
            state.totalScore <= rows[index].upper) {
            HPEN highlight = CreatePen(PS_SOLID, 1, RGB(95, 200, 240));
            HGDIOBJ oldPen = SelectObject(dc, highlight);
            HGDIOBJ oldBrush = SelectObject(dc, GetStockObject(NULL_BRUSH));
            Rectangle(dc, 17, top + 1, 213, bottom);
            SelectObject(dc, oldBrush);
            SelectObject(dc, oldPen);
            DeleteObject(highlight);
        }
        RECT range = { 20, top, 112, bottom };
        RECT name = { 116, top, 211, bottom };
        DrawCombatPanelTextShadowed(dc, rows[index].range, range, 11,
            FW_NORMAL, RGB(190, 211, 224),
            DT_CENTER | DT_VCENTER | DT_SINGLELINE);
        DrawCombatPanelTextShadowed(dc, rows[index].name, name, 11,
            FW_BOLD, RGB(236, 218, 170),
            DT_CENTER | DT_VCENTER | DT_SINGLELINE);
    }
}

void DrawCombatPanelGuide(HDC dc, const DNF90CombatPanelState& state)
{
    RECT client = { 0, 0, kCombatPanelGuideWidth, kCombatPanelGuideHeight };
    HBRUSH background = CreateSolidBrush(RGB(5, 16, 27));
    FillRect(dc, &client, background);
    DeleteObject(background);

    HBRUSH inner = CreateSolidBrush(RGB(8, 29, 45));
    HPEN outerPen = CreatePen(PS_SOLID, 1, RGB(123, 177, 202));
    HGDIOBJ oldBrush = SelectObject(dc, inner);
    HGDIOBJ oldPen = SelectObject(dc, outerPen);
    Rectangle(dc, 1, 1, client.right - 1, client.bottom - 1);
    SelectObject(dc, oldPen);
    SelectObject(dc, oldBrush);
    DeleteObject(outerPen);
    DeleteObject(inner);

    HBRUSH titleBrush = CreateSolidBrush(RGB(12, 55, 80));
    RECT titleBackground = { 4, 4, client.right - 4, 32 };
    FillRect(dc, &titleBackground, titleBrush);
    DeleteObject(titleBrush);
    RECT title = { 8, 4, client.right - 8, 32 };
    DrawCombatPanelTextShadowed(dc, L"战力提升攻略", title, 14,
        FW_BOLD, RGB(229, 242, 247),
        DT_CENTER | DT_VCENTER | DT_SINGLELINE);

    wchar_t summary[96] = {};
    const bool baseValid =
        (state.validFlags & DNF90_COMBAT_PANEL_BASE_SCORE_VALID) != 0;
    unsigned int equipmentBonusHundredths = 0;
    const bool equipmentBonusValid =
        TryComputeCombatPowerEquipmentBonusHundredths(
            state, &equipmentBonusHundredths);
    if (baseValid && equipmentBonusValid) {
        swprintf_s(summary, L"当前：%u（%s）    装备：%u.%02u%%",
            state.totalScore, CombatPowerRankName(state.totalScore),
            equipmentBonusHundredths / 100,
            equipmentBonusHundredths % 100);
    } else if (baseValid) {
        swprintf_s(summary, L"当前：%u（%s）",
            state.totalScore, CombatPowerRankName(state.totalScore));
    } else {
        wcscpy_s(summary, L"正在读取当前角色战力...");
    }
    RECT summaryRect = { 12, 38, client.right - 12, 64 };
    DrawCombatPanelTextShadowed(dc, summary, summaryRect, 11,
        FW_BOLD, RGB(255, 219, 105),
        DT_CENTER | DT_VCENTER | DT_SINGLELINE);

    HPEN separator = CreatePen(PS_SOLID, 1, RGB(38, 85, 110));
    oldPen = SelectObject(dc, separator);
    MoveToEx(dc, 10, 67, nullptr);
    LineTo(dc, client.right - 10, 67);
    SelectObject(dc, oldPen);
    DeleteObject(separator);

    const wchar_t* guideLines[] = {
        L"1. 优先补齐全部装备、称号、时装与光环",
        L"2. 宠物和所有宠物装备都会计入评分",
        L"3. 强化、增幅、套装和高品级装备可提分",
        L"4. 白字可叠加；普通黄字、爆伤只取最高",
        L"5. 黄追、爆追可叠加；所有攻击归入三攻",
    };
    for (size_t index = 0;
         index < sizeof(guideLines) / sizeof(guideLines[0]); ++index) {
        RECT line = { 15, 73 + static_cast<LONG>(index) * 28,
            client.right - 12, 99 + static_cast<LONG>(index) * 28 };
        DrawCombatPanelTextShadowed(dc, guideLines[index], line, 11,
            FW_NORMAL, RGB(205, 226, 236),
            DT_LEFT | DT_VCENTER | DT_SINGLELINE);
    }

    RECT footer = { 10, client.bottom - 27,
        client.right - 10, client.bottom - 7 };
    DrawCombatPanelTextShadowed(dc, L"再次点击“战力提升”关闭", footer, 10,
        FW_NORMAL, RGB(105, 172, 202),
        DT_CENTER | DT_VCENTER | DT_SINGLELINE);
}

DNF90CombatPanelState CopyCombatPanelState()
{
    DNF90CombatPanelState state = {};
    AcquireSRWLockShared(&g_combatPanelStateLock);
    state = g_combatPanelState;
    ReleaseSRWLockShared(&g_combatPanelStateLock);
    return state;
}

void SetCombatPanelGuideVisible(HWND parent, bool visible);
void SetCombatPanelRankTooltipVisible(HWND parent, bool visible);

bool IsCombatPanelUpgradeButtonPoint(POINT point)
{
    RECT button = {
        kCombatPanelUpgradeButtonLeft,
        kCombatPanelUpgradeButtonTop,
        kCombatPanelUpgradeButtonRight,
        kCombatPanelUpgradeButtonBottom,
    };
    return PtInRect(&button, point) != FALSE;
}

LRESULT CALLBACK CombatPanelOverlayWindowProc(
    HWND window, UINT message, WPARAM wParam, LPARAM lParam)
{
    switch (message) {
    case WM_ERASEBKGND:
        return 1;
    case WM_NCHITTEST: {
        if (window == g_combatPanelOverlayWindow) {
            POINT point = {
                static_cast<short>(LOWORD(lParam)),
                static_cast<short>(HIWORD(lParam)),
            };
            if (ScreenToClient(window, &point) &&
                IsCombatPanelUpgradeButtonPoint(point)) {
                return HTCLIENT;
            }
        }
        return HTTRANSPARENT;
    }
    case WM_MOUSEACTIVATE:
        return MA_NOACTIVATE;
    case WM_SETCURSOR:
        if (window == g_combatPanelOverlayWindow) {
            POINT point = {};
            if (GetCursorPos(&point) && ScreenToClient(window, &point) &&
                IsCombatPanelUpgradeButtonPoint(point)) {
                SetCursor(LoadCursorW(nullptr, IDC_HAND));
                return TRUE;
            }
        }
        break;
    case WM_LBUTTONUP:
        if (window == g_combatPanelOverlayWindow) {
            POINT point = {
                static_cast<short>(LOWORD(lParam)),
                static_cast<short>(HIWORD(lParam)),
            };
            if (IsCombatPanelUpgradeButtonPoint(point)) {
                SetCombatPanelGuideVisible(
                    g_dnfWindow, !g_combatPanelGuideVisible);
                return 0;
            }
        }
        break;
    case WM_PAINT: {
        PAINTSTRUCT paint = {};
        HDC dc = BeginPaint(window, &paint);
        RECT client = {};
        GetClientRect(window, &client);
        DNF90CombatPanelState state = CopyCombatPanelState();

        if (window == g_combatPanelRankTooltipWindow) {
            DrawCombatPanelRankTooltip(dc, state);
        } else if (window == g_combatPanelGuideWindow) {
            DrawCombatPanelGuide(dc, state);
        } else {
            DrawCombatPanelMain(dc, state);
        }
        EndPaint(window, &paint);
        return 0;

#if 0  // Preserved only as the V1 fallback layout reference.

        HBRUSH background = CreateSolidBrush(RGB(7, 18, 29));
        FillRect(dc, &client, background);
        DeleteObject(background);

        HBRUSH inner = CreateSolidBrush(RGB(11, 32, 49));
        HPEN outerPen = CreatePen(PS_SOLID, 2, RGB(48, 154, 204));
        HGDIOBJ oldBrush = SelectObject(dc, inner);
        HGDIOBJ oldPen = SelectObject(dc, outerPen);
        RoundRect(dc, 1, 1, client.right - 1, client.bottom - 1, 8, 8);
        SelectObject(dc, oldPen);
        SelectObject(dc, oldBrush);
        DeleteObject(outerPen);
        DeleteObject(inner);

        HBRUSH titleBrush = CreateSolidBrush(RGB(13, 48, 72));
        RECT titleBackground = { 3, 3, client.right - 3, 29 };
        FillRect(dc, &titleBackground, titleBrush);
        DeleteObject(titleBrush);
        RECT title = { 7, 4, client.right - 7, 28 };
        DrawCombatPanelText(dc, L"我的战斗力", title, 14, FW_BOLD,
            RGB(224, 245, 255), DT_CENTER | DT_VCENTER | DT_SINGLELINE);

        // The first implementation deliberately uses an owned GDI emblem.
        // It is independent from PVF resources and therefore cannot break the
        // native personal panel when a custom texture is absent.
        HBRUSH medalOuter = CreateSolidBrush(RGB(181, 119, 46));
        HPEN medalPen = CreatePen(PS_SOLID, 2, RGB(248, 207, 112));
        oldBrush = SelectObject(dc, medalOuter);
        oldPen = SelectObject(dc, medalPen);
        Ellipse(dc, 34, 36, 98, 100);
        SelectObject(dc, oldPen);
        SelectObject(dc, oldBrush);
        DeleteObject(medalPen);
        DeleteObject(medalOuter);
        HBRUSH medalInner = CreateSolidBrush(RGB(41, 126, 183));
        oldBrush = SelectObject(dc, medalInner);
        Ellipse(dc, 43, 45, 89, 91);
        SelectObject(dc, oldBrush);
        DeleteObject(medalInner);
        RECT medalText = { 43, 45, 89, 91 };
        DrawCombatPanelText(dc, L"战", medalText, 24, FW_BOLD,
            RGB(238, 251, 255), DT_CENTER | DT_VCENTER | DT_SINGLELINE);

        const bool baseValid =
            (state.validFlags & DNF90_COMBAT_PANEL_BASE_SCORE_VALID) != 0;
        wchar_t line[64] = {};
        if (baseValid) {
            swprintf_s(line, L"%u", state.totalScore);
        } else {
            wcscpy_s(line, L"读取中...");
        }
        RECT total = { 5, 103, client.right - 5, 132 };
        DrawCombatPanelText(dc, line, total, 24, FW_BOLD,
            RGB(255, 224, 116), DT_CENTER | DT_VCENTER | DT_SINGLELINE);

        const wchar_t* rank = baseValid
            ? CombatPowerRankName(state.totalScore)
            : L"等待角色属性";
        RECT rankRect = { 5, 132, client.right - 5, 153 };
        DrawCombatPanelText(dc, rank, rankRect, 13, FW_NORMAL,
            RGB(178, 216, 236), DT_CENTER | DT_VCENTER | DT_SINGLELINE);

        HPEN separator = CreatePen(PS_SOLID, 1, RGB(30, 84, 116));
        oldPen = SelectObject(dc, separator);
        MoveToEx(dc, 7, 158, nullptr);
        LineTo(dc, client.right - 7, 158);
        SelectObject(dc, oldPen);
        DeleteObject(separator);

        RECT baseLabel = { 8, 164, client.right - 8, 184 };
        DrawCombatPanelText(dc, L"基础属性分", baseLabel, 13, FW_BOLD,
            RGB(166, 222, 246), DT_LEFT | DT_VCENTER | DT_SINGLELINE);
        if (baseValid) {
            swprintf_s(line, L"%u", state.baseAttributeScore);
        } else {
            wcscpy_s(line, L"--");
        }
        RECT baseScore = { 8, 184, client.right - 8, 207 };
        DrawCombatPanelText(dc, line, baseScore, 17, FW_BOLD,
            RGB(112, 221, 255), DT_CENTER | DT_VCENTER | DT_SINGLELINE);

        RECT equipmentLabel = { 8, 211, client.right - 8, 231 };
        DrawCombatPanelText(dc, L"装备加成", equipmentLabel, 13, FW_BOLD,
            RGB(166, 222, 246), DT_LEFT | DT_VCENTER | DT_SINGLELINE);
        if ((state.validFlags &
                DNF90_COMBAT_PANEL_EQUIPMENT_SCORE_VALID) != 0) {
            swprintf_s(line, L"%u", state.equipmentScore);
        } else {
            wcscpy_s(line, L"待接入");
        }
        RECT equipmentScore = { 8, 231, client.right - 8, 253 };
        DrawCombatPanelText(dc, line, equipmentScore, 14, FW_BOLD,
            RGB(244, 190, 93), DT_CENTER | DT_VCENTER | DT_SINGLELINE);

        if (baseValid) {
            swprintf_s(line, L"力量  %d", state.strength);
            RECT statLine = { 10, 260, client.right - 8, 279 };
            DrawCombatPanelText(dc, line, statLine, 12, FW_NORMAL,
                RGB(202, 226, 238), DT_LEFT | DT_VCENTER | DT_SINGLELINE);
            swprintf_s(line, L"智力  %d", state.intelligence);
            statLine.top += 20; statLine.bottom += 20;
            DrawCombatPanelText(dc, line, statLine, 12, FW_NORMAL,
                RGB(202, 226, 238), DT_LEFT | DT_VCENTER | DT_SINGLELINE);
            swprintf_s(line, L"体力  %d", state.vitality);
            statLine.top += 20; statLine.bottom += 20;
            DrawCombatPanelText(dc, line, statLine, 12, FW_NORMAL,
                RGB(202, 226, 238), DT_LEFT | DT_VCENTER | DT_SINGLELINE);
            swprintf_s(line, L"精神  %d", state.spirit);
            statLine.top += 20; statLine.bottom += 20;
            DrawCombatPanelText(dc, line, statLine, 12, FW_NORMAL,
                RGB(202, 226, 238), DT_LEFT | DT_VCENTER | DT_SINGLELINE);
        }

        swprintf_s(line, L"自定义公式 V%u", state.formulaVersion);
        RECT formula = { 6, client.bottom - 24,
            client.right - 6, client.bottom - 6 };
        DrawCombatPanelText(dc, line, formula, 10, FW_NORMAL,
            RGB(91, 145, 170), DT_CENTER | DT_VCENTER | DT_SINGLELINE);
        EndPaint(window, &paint);
        return 0;
#endif
    }
    case WM_NCDESTROY:
        if (g_combatPanelOverlayWindow == window) {
            g_combatPanelOverlayWindow = nullptr;
        }
        if (g_combatPanelRankTooltipWindow == window) {
            g_combatPanelRankTooltipWindow = nullptr;
            g_combatPanelRankTooltipVisible = false;
        }
        if (g_combatPanelGuideWindow == window) {
            g_combatPanelGuideWindow = nullptr;
            g_combatPanelGuideVisible = false;
        }
        break;
    default:
        break;
    }
    return DefWindowProcW(window, message, wParam, lParam);
}

bool EnsureCombatPanelOverlayWindow(HWND parent)
{
    if (g_combatPanelOverlayWindow &&
        IsWindow(g_combatPanelOverlayWindow) &&
        g_combatPanelRankTooltipWindow &&
        IsWindow(g_combatPanelRankTooltipWindow) &&
        g_combatPanelGuideWindow &&
        IsWindow(g_combatPanelGuideWindow)) {
        return true;
    }

    HINSTANCE module = reinterpret_cast<HINSTANCE>(
        GetModuleHandleW(L"90CN.dll"));
    WNDCLASSEXW windowClass = {};
    windowClass.cbSize = sizeof(windowClass);
    windowClass.style = CS_HREDRAW | CS_VREDRAW;
    windowClass.lpfnWndProc = &CombatPanelOverlayWindowProc;
    windowClass.hInstance = module;
    windowClass.lpszClassName = kCombatPanelOverlayClassName;
    if (!RegisterClassExW(&windowClass) &&
        GetLastError() != ERROR_CLASS_ALREADY_EXISTS) {
        LogLine("combat-panel overlay class registration failed error=%lu",
            GetLastError());
        return false;
    }

    const DWORD interactiveStyle = WS_EX_LAYERED |
        WS_EX_NOACTIVATE | WS_EX_TOOLWINDOW;
    const DWORD passiveStyle = interactiveStyle | WS_EX_TRANSPARENT;
    if (!g_combatPanelOverlayWindow ||
        !IsWindow(g_combatPanelOverlayWindow)) {
        HWND overlay = CreateWindowExW(
            interactiveStyle, kCombatPanelOverlayClassName, L"", WS_POPUP,
            0, 0, 0, 0, parent, nullptr, module, nullptr);
        if (!overlay) {
            LogLine("combat-panel overlay creation failed error=%lu",
                GetLastError());
            return false;
        }
        if (!SetLayeredWindowAttributes(overlay, 0, 250, LWA_ALPHA)) {
            LogLine("combat-panel overlay alpha setup failed error=%lu",
                GetLastError());
            DestroyWindow(overlay);
            return false;
        }
        g_combatPanelOverlayWindow = overlay;
    }
    if (!g_combatPanelRankTooltipWindow ||
        !IsWindow(g_combatPanelRankTooltipWindow)) {
        HWND tooltip = CreateWindowExW(
            passiveStyle, kCombatPanelOverlayClassName, L"", WS_POPUP,
            0, 0, 0, 0, parent, nullptr, module, nullptr);
        if (!tooltip) {
            LogLine("combat-panel rank tooltip creation failed error=%lu",
                GetLastError());
            return false;
        }
        if (!SetLayeredWindowAttributes(tooltip, 0, 252, LWA_ALPHA)) {
            LogLine("combat-panel rank tooltip alpha setup failed error=%lu",
                GetLastError());
            DestroyWindow(tooltip);
            return false;
        }
        g_combatPanelRankTooltipWindow = tooltip;
    }
    if (!g_combatPanelGuideWindow ||
        !IsWindow(g_combatPanelGuideWindow)) {
        HWND guide = CreateWindowExW(
            passiveStyle, kCombatPanelOverlayClassName, L"", WS_POPUP,
            0, 0, 0, 0, parent, nullptr, module, nullptr);
        if (!guide) {
            LogLine("combat-panel guide creation failed error=%lu",
                GetLastError());
            return false;
        }
        if (!SetLayeredWindowAttributes(guide, 0, 252, LWA_ALPHA)) {
            LogLine("combat-panel guide alpha setup failed error=%lu",
                GetLastError());
            DestroyWindow(guide);
            return false;
        }
        g_combatPanelGuideWindow = guide;
    }
    return true;
}

bool IsNativePersonalInfoPanelOpen()
{
    if (!g_dnfBase) return false;
    __try {
        void* sceneRoot = *reinterpret_cast<void**>(
            g_dnfBase + kSceneRootPointerRva);
        SceneUiIsOpenFn isOpen = reinterpret_cast<SceneUiIsOpenFn>(
            g_dnfBase + kSceneUiIsOpenRva);
        // Opening equipment while personal information is still registered
        // hides the latter natively. Suppress the sidecar for that overlap so
        // it never floats above the inventory window.
        return sceneRoot && isOpen &&
            isOpen(sceneRoot, kPersonalInfoUiOwner) &&
            !isOpen(sceneRoot, kAvatarPanelUiOwner);
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("combat-panel native personal visibility exception code=0x%08X",
            GetExceptionCode());
        return false;
    }
}

bool TryGetNativePersonalPanelRect(
    int parentWidth, int parentHeight,
    int* panelX, int* panelY, int* panelWidth, int* panelHeight)
{
    if (!g_dnfBase || !panelX || !panelY ||
        !panelWidth || !panelHeight) {
        return false;
    }

    __try {
        uintptr_t sceneRoot = *reinterpret_cast<uintptr_t*>(
            g_dnfBase + kSceneRootPointerRva);
        if (!sceneRoot) return false;
        uintptr_t vector = sceneRoot + kSceneUiOwnerVectorOffset +
            kPersonalInfoUiOwner * kSceneUiOwnerVectorStride;
        uintptr_t begin = *reinterpret_cast<uintptr_t*>(
            vector + kSceneUiOwnerVectorBeginOffset);
        uintptr_t end = *reinterpret_cast<uintptr_t*>(
            vector + kSceneUiOwnerVectorEndOffset);
        if (!begin || end <= begin || end - begin > 64 ||
            ((end - begin) % sizeof(uintptr_t)) != 0) {
            return false;
        }
        uintptr_t panel = *reinterpret_cast<uintptr_t*>(begin);
        uintptr_t widget = panel
            ? *reinterpret_cast<uintptr_t*>(
                panel + kPersonalPanelRootWidgetOffset)
            : 0;
        if (!widget) return false;

        int x = *reinterpret_cast<int*>(
            widget + kPersonalPanelWidgetXOffset);
        int y = *reinterpret_cast<int*>(
            widget + kPersonalPanelWidgetYOffset);
        int width = *reinterpret_cast<int*>(
            widget + kPersonalPanelWidgetWidthOffset);
        int height = *reinterpret_cast<int*>(
            widget + kPersonalPanelWidgetHeightOffset);
        if (x < -64 || y < -64 || width < 160 || height < 200 ||
            width > 640 || height > 760 ||
            x > parentWidth || y > parentHeight) {
            return false;
        }
        *panelX = x;
        *panelY = y;
        *panelWidth = width;
        *panelHeight = height;
        return true;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        return false;
    }
}

bool PositionCombatPanelOverlay(HWND parent)
{
    if (!g_combatPanelOverlayWindow) return false;
    RECT parentClient = {};
    if (!GetClientRect(parent, &parentClient)) return false;
    const int parentWidth = parentClient.right - parentClient.left;
    const int parentHeight = parentClient.bottom - parentClient.top;

    // Use the native D3D widget rectangle so the sidecar follows a dragged
    // personal-information panel. Retain the old bounded center fallback only
    // if the current object layout fails its strict sanity checks.
    int x = parentWidth / 2 + 40;
    int y = parentHeight / 2 - 110;
    int nativeX = 0;
    int nativeY = 0;
    int nativeWidth = 0;
    int nativeHeight = 0;
    if (TryGetNativePersonalPanelRect(
            parentWidth, parentHeight,
            &nativeX, &nativeY, &nativeWidth, &nativeHeight)) {
        x = nativeX + nativeWidth + kPersonalPanelFrameRight;
        y = nativeY - kPersonalPanelFrameTop;
        if (x + kCombatPanelWidth > parentWidth - 4) {
            x = nativeX - kCombatPanelWidth;
        }
    }
    if (x + kCombatPanelWidth > parentWidth - 4) {
        x = parentWidth - kCombatPanelWidth - 4;
    }
    if (x < 4) x = 4;
    if (y + kCombatPanelHeight > parentHeight - 4) {
        y = parentHeight - kCombatPanelHeight - 4;
    }
    if (y < 4) y = 4;

    POINT screenPosition = { x, y };
    if (!ClientToScreen(parent, &screenPosition)) return false;
    // hWndInsertAfter=parent places this popup behind the DirectX owner. Keep
    // it at the top of the active process's non-topmost band instead.
    return SetWindowPos(g_combatPanelOverlayWindow, HWND_TOP,
        screenPosition.x, screenPosition.y,
        kCombatPanelWidth, kCombatPanelHeight,
        SWP_NOACTIVATE | SWP_SHOWWINDOW) != FALSE;
}

void SetCombatPanelGuideVisible(HWND parent, bool visible)
{
    if (!visible || !parent || !g_combatPanelOverlayWindow ||
        !g_combatPanelGuideWindow) {
        if (g_combatPanelGuideWindow) {
            ShowWindow(g_combatPanelGuideWindow, SW_HIDE);
        }
        g_combatPanelGuideVisible = false;
        return;
    }

    RECT panel = {};
    RECT parentRect = {};
    if (!GetWindowRect(g_combatPanelOverlayWindow, &panel) ||
        !GetWindowRect(parent, &parentRect)) {
        return;
    }
    int x = panel.right + 4;
    int y = panel.top + kCombatPanelUpgradeButtonTop - 4;
    if (x + kCombatPanelGuideWidth > parentRect.right - 4) {
        x = panel.left - kCombatPanelGuideWidth - 4;
    }
    if (x < parentRect.left + 4) x = parentRect.left + 4;
    if (y + kCombatPanelGuideHeight > parentRect.bottom - 4) {
        y = parentRect.bottom - kCombatPanelGuideHeight - 4;
    }
    if (y < parentRect.top + 4) y = parentRect.top + 4;

    SetCombatPanelRankTooltipVisible(parent, false);
    const bool wasVisible =
        IsWindowVisible(g_combatPanelGuideWindow) != FALSE;
    SetWindowPos(g_combatPanelGuideWindow, HWND_TOP,
        x, y, kCombatPanelGuideWidth, kCombatPanelGuideHeight,
        SWP_NOACTIVATE | SWP_SHOWWINDOW);
    if (!wasVisible) {
        InvalidateRect(g_combatPanelGuideWindow, nullptr, FALSE);
        UpdateWindow(g_combatPanelGuideWindow);
    }
    g_combatPanelGuideVisible = true;
}

void SetCombatPanelRankTooltipVisible(HWND parent, bool visible)
{
    if (!visible || !parent || !g_combatPanelOverlayWindow ||
        !g_combatPanelRankTooltipWindow) {
        if (g_combatPanelRankTooltipWindow) {
            ShowWindow(g_combatPanelRankTooltipWindow, SW_HIDE);
        }
        g_combatPanelRankTooltipVisible = false;
        return;
    }

    RECT panel = {};
    RECT parentRect = {};
    if (!GetWindowRect(g_combatPanelOverlayWindow, &panel) ||
        !GetWindowRect(parent, &parentRect)) {
        return;
    }
    int x = panel.right + 4;
    int y = panel.top + 34;
    if (x + kCombatPanelRankTooltipWidth > parentRect.right - 4) {
        x = panel.left - kCombatPanelRankTooltipWidth - 4;
    }
    if (x < parentRect.left + 4) x = parentRect.left + 4;
    if (y + kCombatPanelRankTooltipHeight > parentRect.bottom - 4) {
        y = parentRect.bottom - kCombatPanelRankTooltipHeight - 4;
    }
    if (y < parentRect.top + 4) y = parentRect.top + 4;

    const bool wasVisible =
        IsWindowVisible(g_combatPanelRankTooltipWindow) != FALSE;
    SetWindowPos(g_combatPanelRankTooltipWindow, HWND_TOP,
        x, y, kCombatPanelRankTooltipWidth, kCombatPanelRankTooltipHeight,
        SWP_NOACTIVATE | SWP_SHOWWINDOW);
    if (!wasVisible) {
        InvalidateRect(g_combatPanelRankTooltipWindow, nullptr, FALSE);
        UpdateWindow(g_combatPanelRankTooltipWindow);
    }
    g_combatPanelRankTooltipVisible = true;
}

void RefreshCombatPanelRankTooltip(HWND parent)
{
    if (g_combatPanelGuideVisible || !g_combatPanelOverlayWindow ||
        !IsWindowVisible(g_combatPanelOverlayWindow)) {
        SetCombatPanelRankTooltipVisible(parent, false);
        return;
    }
    RECT hover = {};
    POINT cursor = {};
    if (!GetWindowRect(g_combatPanelOverlayWindow, &hover) ||
        !GetCursorPos(&cursor)) {
        SetCombatPanelRankTooltipVisible(parent, false);
        return;
    }
    hover.top += kCombatPanelRankHoverTop;
    hover.bottom = hover.top +
        (kCombatPanelRankHoverBottom - kCombatPanelRankHoverTop);
    SetCombatPanelRankTooltipVisible(parent,
        PtInRect(&hover, cursor) != FALSE);
}

void RefreshCombatPanelOverlay(HWND parent)
{
    if (!parent || IsIconic(parent) || GetForegroundWindow() != parent) {
        if (g_combatPanelOverlayWindow) {
            ShowWindow(g_combatPanelOverlayWindow, SW_HIDE);
        }
        SetCombatPanelRankTooltipVisible(parent, false);
        SetCombatPanelGuideVisible(parent, false);
        return;
    }

    const bool personalInfoOpen = IsNativePersonalInfoPanelOpen();
    DNF90CombatPanelState state = CopyCombatPanelState();
    const bool enabled =
        (state.validFlags & DNF90_COMBAT_PANEL_ENABLED) != 0;
    if (!personalInfoOpen || !enabled) {
        if (g_combatPanelOverlayWindow &&
            IsWindowVisible(g_combatPanelOverlayWindow)) {
            ShowWindow(g_combatPanelOverlayWindow, SW_HIDE);
        }
        SetCombatPanelRankTooltipVisible(parent, false);
        SetCombatPanelGuideVisible(parent, false);
        if (g_combatPanelPersonalInfoOpen != personalInfoOpen) {
            LogLine("combat-panel personal visibility open=%d enabled=%d",
                personalInfoOpen ? 1 : 0, enabled ? 1 : 0);
        }
        g_combatPanelPersonalInfoOpen = personalInfoOpen;
        return;
    }

    if (!EnsureCombatPanelOverlayWindow(parent)) return;
    const bool wasVisible =
        IsWindowVisible(g_combatPanelOverlayWindow) != FALSE;
    PositionCombatPanelOverlay(parent);
    if (g_combatPanelGuideVisible) {
        SetCombatPanelGuideVisible(parent, true);
    }
    RefreshCombatPanelRankTooltip(parent);
    if (!wasVisible) {
        InvalidateRect(g_combatPanelOverlayWindow, nullptr, FALSE);
    }
    if (!g_combatPanelPersonalInfoOpen) {
        LogLine("combat-panel shown revision=%u generation=%u score=%u "
            "base_valid=%d equipment_valid=%d",
            state.revision, state.sourceGeneration, state.totalScore,
            (state.validFlags &
                DNF90_COMBAT_PANEL_BASE_SCORE_VALID) ? 1 : 0,
            (state.validFlags &
                DNF90_COMBAT_PANEL_EQUIPMENT_SCORE_VALID) ? 1 : 0);
    }
    g_combatPanelPersonalInfoOpen = true;
}

bool QueueLuaClientNoticeInternal(const wchar_t* text, unsigned int length)
{
    if (!text || length == 0 || length > kLuaNoticeMaximumCharacters) {
        return RejectLuaClientNotice("invalid-length");
    }
    HWND bridgeWindow = reinterpret_cast<HWND>(
        InterlockedCompareExchangePointer(
            &g_luaNoticeBridgeWindow, nullptr, nullptr));
    UINT bridgeMessage = static_cast<UINT>(
        InterlockedCompareExchange(&g_luaNoticeWindowMessage, 0, 0));
    if (!bridgeWindow || !bridgeMessage) {
        return RejectLuaClientNotice("main-thread-bridge-not-ready");
    }

    LuaClientNotice pending = {};
    pending.length = length;
    __try {
        for (unsigned int index = 0; index < length; ++index) {
            wchar_t value = text[index];
            if (value == L'\0') {
                return RejectLuaClientNotice("embedded-nul");
            }
            pending.text[index] = value;
        }
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        return RejectLuaClientNotice("unreadable-text");
    }
    pending.text[length] = L'\0';

    AcquireSRWLockExclusive(&g_luaNoticeLock);
    if (g_luaNoticeCount >= kLuaNoticeQueueCapacity) {
        ReleaseSRWLockExclusive(&g_luaNoticeLock);
        return RejectLuaClientNotice("queue-full");
    }
    size_t tail = (g_luaNoticeHead + g_luaNoticeCount) %
        kLuaNoticeQueueCapacity;
    g_luaNoticeQueue[tail] = pending;
    ++g_luaNoticeCount;
    ReleaseSRWLockExclusive(&g_luaNoticeLock);

    LONG queued = InterlockedIncrement(&g_luaNoticeQueuedCount);
    if (!PostMessageW(bridgeWindow, bridgeMessage, 0, 0)) {
        LogLine("lua client notice PostMessageW failed queued=%ld error=%lu",
            queued, GetLastError());
        return false;
    }
    LogLine("lua client notice queued=%ld chars=%u caller_tid=%lu",
        queued, length, GetCurrentThreadId());
    return true;
}

bool DequeueLuaClientNotice(LuaClientNotice* output)
{
    if (!output) return false;
    AcquireSRWLockExclusive(&g_luaNoticeLock);
    if (g_luaNoticeCount == 0) {
        ReleaseSRWLockExclusive(&g_luaNoticeLock);
        return false;
    }
    *output = g_luaNoticeQueue[g_luaNoticeHead];
    g_luaNoticeQueue[g_luaNoticeHead] = {};
    g_luaNoticeHead = (g_luaNoticeHead + 1) % kLuaNoticeQueueCapacity;
    --g_luaNoticeCount;
    ReleaseSRWLockExclusive(&g_luaNoticeLock);
    return true;
}

LuaClientEventContext BuildLuaDungeonClientEventContext(
    bool previousRoomKnown = false,
    unsigned int previousRoomX = 0,
    unsigned int previousRoomY = 0,
    unsigned int previousRoomLayerFlag = 0,
    unsigned int previousMapID = 0)
{
    LuaClientEventContext context = {};
    if (g_luaDungeonID != 0) {
        context.flags |= DNF90_CLIENT_EVENT_CONTEXT_DUNGEON_VALID;
        context.dungeonID = g_luaDungeonID;
    }
    if (g_luaDungeonRoomKnown) {
        context.flags |= DNF90_CLIENT_EVENT_CONTEXT_ROOM_VALID;
        context.roomX = g_luaDungeonRoomX;
        context.roomY = g_luaDungeonRoomY;
        context.roomLayerFlag = g_luaDungeonRoomLayerFlag;
        context.mapID = g_luaDungeonMapID;
        if (g_luaDungeonRoomX == g_luaDungeonBossX &&
            g_luaDungeonRoomY == g_luaDungeonBossY) {
            context.flags |= DNF90_CLIENT_EVENT_CONTEXT_BOSS_ROOM;
        }
    }
    if (previousRoomKnown) {
        context.flags |= DNF90_CLIENT_EVENT_CONTEXT_PREVIOUS_ROOM_VALID;
        context.previousRoomX = previousRoomX;
        context.previousRoomY = previousRoomY;
        context.previousRoomLayerFlag = previousRoomLayerFlag;
        context.previousMapID = previousMapID;
    }
    return context;
}

bool QueueLuaClientEventInternal(
    DNF90ClientEventType type, DWORD uiThreadID,
    const LuaClientEventContext* context = nullptr)
{
    DNF90ClientEvent pending = {};
    pending.size = sizeof(pending);
    pending.type = static_cast<unsigned int>(type);
    pending.sequence = static_cast<unsigned int>(
        InterlockedIncrement(&g_luaClientEventSequence));
    pending.processID = GetCurrentProcessId();
    pending.uiThreadID = uiThreadID;
    if (context) {
        pending.contextFlags = context->flags;
        pending.dungeonID = context->dungeonID;
        pending.roomX = context->roomX;
        pending.roomY = context->roomY;
        pending.roomLayerFlag = context->roomLayerFlag;
        pending.mapID = context->mapID;
        pending.previousRoomX = context->previousRoomX;
        pending.previousRoomY = context->previousRoomY;
        pending.previousRoomLayerFlag = context->previousRoomLayerFlag;
        pending.previousMapID = context->previousMapID;
    }

    AcquireSRWLockExclusive(&g_luaClientEventLock);
    if (g_luaClientEventCount >= kLuaClientEventQueueCapacity) {
        ReleaseSRWLockExclusive(&g_luaClientEventLock);
        LONG rejected = InterlockedIncrement(
            &g_luaClientEventRejectedCount);
        LogLine("lua client event rejected=%ld type=%u sequence=%u reason=queue-full",
            rejected, pending.type, pending.sequence);
        return false;
    }
    size_t tail = (g_luaClientEventHead + g_luaClientEventCount) %
        kLuaClientEventQueueCapacity;
    g_luaClientEventQueue[tail] = pending;
    ++g_luaClientEventCount;
    ReleaseSRWLockExclusive(&g_luaClientEventLock);

    LogLine("lua client event queued type=%u sequence=%u ui_tid=%lu pid=%lu "
        "context=0x%X dungeon=%u room=%u:%u layer=%u map=%u",
        pending.type, pending.sequence,
        static_cast<unsigned long>(pending.uiThreadID),
        static_cast<unsigned long>(pending.processID), pending.contextFlags,
        pending.dungeonID, pending.roomX, pending.roomY,
        pending.roomLayerFlag, pending.mapID);
    return true;
}

bool ParseEvidenceBackedDungeonStartMapBody(
    const unsigned char* body,
    size_t bodyLength,
    bool requireInitialRoom,
    unsigned char* roomX,
    unsigned char* roomY,
    unsigned char* roomLayerFlag,
    unsigned int* mapID)
{
    // The smallest valid current op29 body is 23 bytes. Offset 13 is the
    // initial room-state flag, offset 14 the map id, and offset 18 the first
    // variable-table count. Initial entry requires flag 1; later first visits
    // use flag 1 and revisits use flag 0. Parse every counted row so a
    // truncated packet cannot publish a lifecycle event merely by sharing the
    // same opcode.
    if (!body || !roomX || !roomY || !roomLayerFlag || !mapID ||
        bodyLength < 23 || body[2] > 1 || body[13] > 1 ||
        (requireInitialRoom && body[13] != 1)) {
        return false;
    }
    unsigned int parsedMapID =
        *reinterpret_cast<const unsigned int*>(body + 14);
    if (parsedMapID == 0) return false;

    size_t offset = 19;
    size_t actorCount = body[18];
    if (actorCount > (bodyLength - offset) / 21) return false;
    offset += actorCount * 21;

    if (offset >= bodyLength) return false;
    size_t extraCount = body[offset++];
    if (extraCount > (bodyLength - offset) / 21) return false;
    offset += extraCount * 21;

    // Hell-party fog byte followed by the ridable-object group table.
    if (bodyLength - offset < 2) return false;
    ++offset;
    size_t groupCount = body[offset++];
    for (size_t group = 0; group < groupCount; ++group) {
        if (offset >= bodyLength) return false;
        size_t entryCount = body[offset++];
        if (entryCount > (bodyLength - offset) / 20) return false;
        offset += entryCount * 20;
    }

    // Exactly one trailing party-member index completes the typed body.
    if (bodyLength - offset != 1) return false;
    *roomX = body[0];
    *roomY = body[1];
    *roomLayerFlag = body[2];
    *mapID = parsedMapID;
    return true;
}

void UpdateLuaDungeonEntryStateAfterDispatch(
    int packet, unsigned int opcode)
{
    if (opcode != kDungeonInfoOpcode &&
        opcode != kDungeonStartMapOpcode &&
        opcode != kDungeonUserStateOpcode) {
        return;
    }

    unsigned int packetLength = 0;
    size_t bodyLength = 0;
    unsigned int dungeonID = 0;
    unsigned int mapID = 0;
    unsigned char roomX = 0;
    unsigned char roomY = 0;
    unsigned char roomLayerFlag = 0;
    unsigned short objectKey = 0;
    uintptr_t currentActor = 0;
    uintptr_t selectedActor = 0;
    __try {
        if (!packet) return;
        const unsigned char* bytes =
            reinterpret_cast<const unsigned char*>(packet);
        packetLength = *reinterpret_cast<const unsigned int*>(bytes + 3);
        if (packetLength < 16) return;
        const unsigned char* body = bytes + 16;
        bodyLength = packetLength - 16;

        if (opcode == kDungeonInfoOpcode) {
            // The evidence-backed typed body is at least 36 bytes; optional
            // counted groups may extend it. A nonzero dungeon id is required.
            if (bodyLength < 36) return;
            dungeonID = *reinterpret_cast<const unsigned int*>(body);
            if (dungeonID == 0) return;
            g_luaDungeonEntryStage = kLuaDungeonEntryAwaitStartMap;
            g_luaDungeonID = dungeonID;
            g_luaDungeonBossX = body[8];
            g_luaDungeonBossY = body[9];
            g_luaDungeonRoomKnown = false;
            g_luaDungeonRoomX = 0;
            g_luaDungeonRoomY = 0;
            g_luaDungeonRoomLayerFlag = 0;
            g_luaDungeonMapID = 0;
            LogLine("lua enter_dungeon armed stage=op28 packet_len=%u "
                "dungeon=%u boss=%u:%u",
                packetLength, dungeonID,
                g_luaDungeonBossX, g_luaDungeonBossY);
            return;
        }

        if (opcode == kDungeonStartMapOpcode) {
            bool initialRoom = g_luaDungeonEntryStage ==
                kLuaDungeonEntryAwaitStartMap;
            bool changedRoom = g_luaDungeonEntryStage ==
                kLuaDungeonEntryActive;
            if (!initialRoom && !changedRoom) {
                return;
            }
            if (!ParseEvidenceBackedDungeonStartMapBody(
                    body, bodyLength, initialRoom,
                    &roomX, &roomY, &roomLayerFlag, &mapID)) {
                LogLine("lua dungeon lifecycle waiting stage=op29-invalid "
                    "packet_len=%u body_len=%u initial=%u",
                    packetLength, static_cast<unsigned int>(bodyLength),
                    initialRoom ? 1u : 0u);
                return;
            }
            if (initialRoom) {
                g_luaDungeonRoomKnown = true;
                g_luaDungeonRoomX = roomX;
                g_luaDungeonRoomY = roomY;
                g_luaDungeonRoomLayerFlag = roomLayerFlag;
                g_luaDungeonMapID = mapID;
                g_luaDungeonEntryStage = kLuaDungeonEntryAwaitUserState;
                LogLine("lua enter_dungeon armed stage=op29 packet_len=%u "
                    "room=%u:%u layer=%u map=%u",
                    packetLength, roomX, roomY, roomLayerFlag, mapID);
                return;
            }

            if (g_luaDungeonRoomKnown &&
                g_luaDungeonRoomX == roomX &&
                g_luaDungeonRoomY == roomY &&
                g_luaDungeonRoomLayerFlag == roomLayerFlag &&
                g_luaDungeonMapID == mapID) {
                LogLine("lua dungeon_room_changed suppressed reason=duplicate "
                    "packet_len=%u room=%u:%u layer=%u map=%u",
                    packetLength, roomX, roomY, roomLayerFlag, mapID);
                return;
            }
            unsigned char previousRoomX = g_luaDungeonRoomX;
            unsigned char previousRoomY = g_luaDungeonRoomY;
            unsigned char previousRoomLayerFlag =
                g_luaDungeonRoomLayerFlag;
            unsigned int previousMapID = g_luaDungeonMapID;
            bool previousRoomKnown = g_luaDungeonRoomKnown;
            g_luaDungeonRoomKnown = true;
            g_luaDungeonRoomX = roomX;
            g_luaDungeonRoomY = roomY;
            g_luaDungeonRoomLayerFlag = roomLayerFlag;
            g_luaDungeonMapID = mapID;
            LuaClientEventContext context =
                BuildLuaDungeonClientEventContext(
                    previousRoomKnown,
                    previousRoomX, previousRoomY,
                    previousRoomLayerFlag, previousMapID);
            bool queued = QueueLuaClientEventInternal(
                DNF90_CLIENT_EVENT_DUNGEON_ROOM_CHANGED,
                GetCurrentThreadId(), &context);
            LogLine("lua dungeon_room_changed confirmed packet_len=%u "
                "previous_known=%u previous_room=%u:%u previous_layer=%u "
                "previous_map=%u room=%u:%u layer=%u map=%u queued=%u",
                packetLength, previousRoomKnown ? 1u : 0u,
                previousRoomX, previousRoomY, previousRoomLayerFlag,
                previousMapID, roomX, roomY, roomLayerFlag, mapID,
                queued ? 1u : 0u);
            return;
        }

        if (g_luaDungeonEntryStage !=
            kLuaDungeonEntryAwaitUserState || bodyLength != 4 ||
            body[0] != 1 || body[3] != 1) {
            return;
        }
        objectKey = *reinterpret_cast<const unsigned short*>(body + 1);
        if (objectKey == 0) return;

        uintptr_t objectManager = *reinterpret_cast<uintptr_t*>(
            g_dnfBase + kObjectManagerPointerRva);
        CurrentActorFn currentActorFn = reinterpret_cast<CurrentActorFn>(
            g_dnfBase + kCurrentActorRva);
        ActorByObjectKeyFn actorByObjectKeyFn =
            reinterpret_cast<ActorByObjectKeyFn>(
                g_dnfBase + kActorByObjectKeyRva);
        currentActor = objectManager && currentActorFn
            ? reinterpret_cast<uintptr_t>(
                currentActorFn(reinterpret_cast<void*>(objectManager)))
            : 0;
        selectedActor = actorByObjectKeyFn
            ? reinterpret_cast<uintptr_t>(actorByObjectKeyFn(objectKey))
            : 0;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("lua enter_dungeon waiting reason=read-exception "
            "opcode=%u packet_len=%u body_len=%u key=%u code=0x%08X",
            opcode, packetLength, static_cast<unsigned int>(bodyLength),
            objectKey, GetExceptionCode());
        return;
    }

    if (!currentActor || selectedActor != currentActor) {
        LogLine("lua enter_dungeon waiting reason=current-actor-mismatch "
            "packet_len=%u key=%u selected_actor=%p current_actor=%p",
            packetLength, objectKey,
            reinterpret_cast<void*>(selectedActor),
            reinterpret_cast<void*>(currentActor));
        return;
    }

    // Mark active even if the bounded queue is full: this exact transition
    // has already happened and a later room packet must never replay it.
    g_luaDungeonEntryStage = kLuaDungeonEntryActive;
    LuaClientEventContext context =
        BuildLuaDungeonClientEventContext();
    bool queued = QueueLuaClientEventInternal(
        DNF90_CLIENT_EVENT_ENTER_DUNGEON,
        GetCurrentThreadId(), &context);
    LogLine("lua enter_dungeon confirmed packet_len=%u key=%u actor=%p queued=%u",
        packetLength, objectKey, reinterpret_cast<void*>(currentActor),
        queued ? 1u : 0u);
}

void QueueLuaEnterTownEventIfAccepted(
    int packet,
    unsigned int sceneModeCalls,
    unsigned int loadingGateCalls,
    bool sceneModeEarlyReturn,
    bool loadingGateEarlyReturn)
{
    if (sceneModeEarlyReturn || loadingGateEarlyReturn) {
        LogLine("lua enter_town suppressed reason=native-early-return "
            "scene_calls=%u loading_calls=%u scene_rejected=%u loading_rejected=%u",
            sceneModeCalls, loadingGateCalls,
            sceneModeEarlyReturn ? 1u : 0u,
            loadingGateEarlyReturn ? 1u : 0u);
        return;
    }

    unsigned int packetLength = 0;
    unsigned int rowCount = 0;
    unsigned int selectedObjectKey = 0;
    uintptr_t currentActor = 0;
    uintptr_t selectedActor = 0;
    __try {
        if (!packet) return;
        const unsigned char* bytes =
            reinterpret_cast<const unsigned char*>(packet);
        unsigned int opcode =
            *reinterpret_cast<const unsigned short*>(bytes + 1);
        packetLength = *reinterpret_cast<const unsigned int*>(bytes + 3);
        if (opcode != 24 || packetLength < 20) return;

        const unsigned char* body = bytes + 16;
        size_t bodyLength = packetLength - 16;
        rowCount = *reinterpret_cast<const unsigned short*>(body + 2);
        if (rowCount == 0 || rowCount > (bodyLength - 4) / 8) return;
        selectedObjectKey = *reinterpret_cast<const unsigned short*>(
            body + 4 + (rowCount - 1) * 8);
        if (selectedObjectKey == 0) return;

        uintptr_t objectManager = *reinterpret_cast<uintptr_t*>(
            g_dnfBase + kObjectManagerPointerRva);
        CurrentActorFn currentActorFn = reinterpret_cast<CurrentActorFn>(
            g_dnfBase + kCurrentActorRva);
        ActorByObjectKeyFn actorByObjectKeyFn =
            reinterpret_cast<ActorByObjectKeyFn>(
                g_dnfBase + kActorByObjectKeyRva);
        currentActor = objectManager && currentActorFn
            ? reinterpret_cast<uintptr_t>(
                currentActorFn(reinterpret_cast<void*>(objectManager)))
            : 0;
        selectedActor = actorByObjectKeyFn
            ? reinterpret_cast<uintptr_t>(actorByObjectKeyFn(
                static_cast<unsigned short>(selectedObjectKey)))
            : 0;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("lua enter_town suppressed reason=read-exception "
            "packet_len=%u rows=%u key=%u code=0x%08X",
            packetLength, rowCount, selectedObjectKey, GetExceptionCode());
        return;
    }

    if (!currentActor || selectedActor != currentActor) {
        LogLine("lua enter_town suppressed reason=current-actor-mismatch "
            "packet_len=%u rows=%u key=%u selected_actor=%p current_actor=%p",
            packetLength, rowCount, selectedObjectKey,
            reinterpret_cast<void*>(selectedActor),
            reinterpret_cast<void*>(currentActor));
        return;
    }

    // An accepted town transition closes any pending or active dungeon-entry
    // lifecycle. The next event must begin with a fresh typed op28 packet.
    LuaDungeonEntryStage previousDungeonStage = g_luaDungeonEntryStage;
    LuaClientEventContext leaveContext =
        BuildLuaDungeonClientEventContext();
    g_luaDungeonEntryStage = kLuaDungeonEntryIdle;
    g_luaDungeonID = 0;
    g_luaDungeonBossX = 0;
    g_luaDungeonBossY = 0;
    g_luaDungeonRoomKnown = false;
    g_luaDungeonRoomX = 0;
    g_luaDungeonRoomY = 0;
    g_luaDungeonRoomLayerFlag = 0;
    g_luaDungeonMapID = 0;
    bool leaveQueued = false;
    if (previousDungeonStage == kLuaDungeonEntryActive) {
        leaveQueued = QueueLuaClientEventInternal(
            DNF90_CLIENT_EVENT_LEAVE_DUNGEON,
            GetCurrentThreadId(), &leaveContext);
        LogLine("lua leave_dungeon confirmed packet_len=%u key=%u actor=%p queued=%u",
            packetLength, selectedObjectKey,
            reinterpret_cast<void*>(currentActor), leaveQueued ? 1u : 0u);
    }
    bool townQueued = QueueLuaClientEventInternal(
        DNF90_CLIENT_EVENT_ENTER_TOWN, GetCurrentThreadId());
    LogLine("lua enter_town confirmed packet_len=%u rows=%u key=%u actor=%p "
        "scene_calls=%u loading_calls=%u dungeon_stage=%u "
        "leave_queued=%u queued=%u",
        packetLength, rowCount, selectedObjectKey,
        reinterpret_cast<void*>(currentActor), sceneModeCalls,
        loadingGateCalls, static_cast<unsigned int>(previousDungeonStage),
        leaveQueued ? 1u : 0u, townQueued ? 1u : 0u);
}

bool DequeueLuaClientEventInternal(DNF90ClientEvent* output)
{
    if (!output) return false;
    AcquireSRWLockExclusive(&g_luaClientEventLock);
    if (g_luaClientEventCount == 0) {
        ReleaseSRWLockExclusive(&g_luaClientEventLock);
        return false;
    }
    *output = g_luaClientEventQueue[g_luaClientEventHead];
    g_luaClientEventQueue[g_luaClientEventHead] = {};
    g_luaClientEventHead =
        (g_luaClientEventHead + 1) % kLuaClientEventQueueCapacity;
    --g_luaClientEventCount;
    ReleaseSRWLockExclusive(&g_luaClientEventLock);
    return true;
}

void QueueLuaUiClosedEventOnce(DWORD uiThreadID)
{
    if (InterlockedCompareExchange(
            &g_luaUiClosedEventQueued, 1, 0) != 0) {
        return;
    }
    if (!QueueLuaClientEventInternal(
            DNF90_CLIENT_EVENT_UI_CLOSED, uiThreadID)) {
        InterlockedExchange(&g_luaUiClosedEventQueued, 0);
    }
}

LRESULT CALLBACK ProxyDnfWindowProc(
    HWND hWnd, UINT message, WPARAM wParam, LPARAM lParam)
{
    WNDPROC original = g_originalDnfWindowProc;
    if (!original) return DefWindowProcW(hWnd, message, wParam, lParam);

    // Do not treat WM_CLOSE as final because the client may cancel a close
    // request. WM_DESTROY is the first confirmed shutdown boundary and
    // WM_NCDESTROY is its idempotent fallback.
    if (message == WM_DESTROY || message == WM_NCDESTROY) {
        QueueLuaUiClosedEventOnce(GetCurrentThreadId());
        if (message == WM_DESTROY) {
            KillTimer(hWnd, kCombatPanelPollTimer);
            if (g_combatPanelGuideWindow &&
                IsWindow(g_combatPanelGuideWindow)) {
                DestroyWindow(g_combatPanelGuideWindow);
            }
            if (g_combatPanelRankTooltipWindow &&
                IsWindow(g_combatPanelRankTooltipWindow)) {
                DestroyWindow(g_combatPanelRankTooltipWindow);
            }
            if (g_combatPanelOverlayWindow &&
                IsWindow(g_combatPanelOverlayWindow)) {
                DestroyWindow(g_combatPanelOverlayWindow);
            }
            g_combatPanelGuideWindow = nullptr;
            g_combatPanelGuideVisible = false;
            g_combatPanelRankTooltipWindow = nullptr;
            g_combatPanelRankTooltipVisible = false;
            g_combatPanelOverlayWindow = nullptr;
            g_combatPanelPersonalInfoOpen = false;
        }
        if (message == WM_NCDESTROY) {
            InterlockedExchangePointer(&g_luaNoticeBridgeWindow, nullptr);
            InterlockedExchange(&g_luaNoticeWindowMessage, 0);
            InterlockedExchange(&g_combatPanelWindowMessage, 0);
            g_dnfWindow = nullptr;
        }
    }

    if (message == WM_TIMER && wParam == kCombatPanelPollTimer) {
        RefreshCombatPanelOverlay(hWnd);
        return 0;
    }

    if ((message == WM_MOVE || message == WM_SIZE) &&
        g_combatPanelOverlayWindow &&
        IsWindowVisible(g_combatPanelOverlayWindow)) {
        PositionCombatPanelOverlay(hWnd);
        if (g_combatPanelGuideVisible) {
            SetCombatPanelGuideVisible(hWnd, true);
        }
    }

    UINT noticeMessage = static_cast<UINT>(
        InterlockedCompareExchange(&g_luaNoticeWindowMessage, 0, 0));
    if (noticeMessage && message == noticeMessage) {
        LuaClientNotice notice = {};
        if (!DequeueLuaClientNotice(&notice)) return 0;

        __try {
            if (ShowLuaClientNoticeOverlay(hWnd, notice)) {
                LONG dispatched = InterlockedIncrement(
                    &g_luaNoticeDispatchCount);
                LogLine("lua client notice dispatched=%ld chars=%u ui_tid=%lu renderer=owned-popup-overlay",
                    dispatched, notice.length, GetCurrentThreadId());
            } else {
                RejectLuaClientNotice("overlay-render-failed");
            }
        }
        __except (EXCEPTION_EXECUTE_HANDLER) {
            LogLine("lua client notice dispatch exception code=0x%08X chars=%u ui_tid=%lu",
                GetExceptionCode(), notice.length, GetCurrentThreadId());
        }
        return 0;
    }

    UINT combatPanelMessage = static_cast<UINT>(
        InterlockedCompareExchange(&g_combatPanelWindowMessage, 0, 0));
    if (combatPanelMessage && message == combatPanelMessage) {
        RefreshCombatPanelOverlay(hWnd);
        if (g_combatPanelOverlayWindow) {
            InvalidateRect(g_combatPanelOverlayWindow, nullptr, FALSE);
        }
        if (g_combatPanelRankTooltipWindow &&
            IsWindowVisible(g_combatPanelRankTooltipWindow)) {
            InvalidateRect(g_combatPanelRankTooltipWindow, nullptr, FALSE);
        }
        if (g_combatPanelGuideWindow &&
            IsWindowVisible(g_combatPanelGuideWindow)) {
            InvalidateRect(g_combatPanelGuideWindow, nullptr, FALSE);
        }
        return 0;
    }

    if (message == WM_RBUTTONDOWN || message == WM_RBUTTONDBLCLK) {
        LONG before = InterlockedCompareExchange(
            &g_clientOp44SendCount, 0, 0);
        CurrentContractSelection selection = {};
        bool selected = ResolveHoveredPremiumContractSelection(&selection);
        LRESULT result = CallWindowProcW(
            original, hWnd, message, wParam, lParam);
        if (!selected) {
            selected = ResolveHoveredPremiumContractSelection(&selection);
        }

        g_contractRButtonPending = selected;
        g_contractRButtonPendingSlot = selected ? selection.slot : -1;
        g_contractRButtonPendingTemplateID =
            selected ? selection.templateID : 0;
        g_contractRButtonPendingIdentity = selected ? selection.identity : 0;
        g_contractRButtonPendingOp44Count = before;
        if (selected) {
            LogLine("contract-use right-button down slot=%d item=%lu identity=%lu before=%ld",
                selection.slot,
                static_cast<unsigned long>(selection.templateID),
                static_cast<unsigned long>(selection.identity), before);
        }
        return result;
    }

    if (message == WM_RBUTTONUP) {
        LRESULT result = CallWindowProcW(
            original, hWnd, message, wParam, lParam);
        if (!g_contractRButtonPending) return result;

        CurrentContractSelection selection = {};
        selection.slot = g_contractRButtonPendingSlot;
        selection.templateID = g_contractRButtonPendingTemplateID;
        selection.identity = g_contractRButtonPendingIdentity;
        LONG before = g_contractRButtonPendingOp44Count;
        g_contractRButtonPending = false;
        g_contractRButtonPendingSlot = -1;
        g_contractRButtonPendingTemplateID = 0;
        g_contractRButtonPendingIdentity = 0;

        LONG after = InterlockedCompareExchange(
            &g_clientOp44SendCount, 0, 0);
        if (after == before && GetForegroundWindow() == hWnd) {
            g_contractUseFallbackActive = true;
            bool sent = SendCurrentPremiumContractUse(selection);
            g_contractUseFallbackActive = false;
            if (sent) {
                LONG hit = InterlockedIncrement(
                    &g_contractRButtonFallbackCount);
                LogLine("contract-use right-button fallback hit=%ld slot=%d item=%lu identity=%lu",
                    hit, selection.slot,
                    static_cast<unsigned long>(selection.templateID),
                    static_cast<unsigned long>(selection.identity));
            }
        }
        return result;
    }

    return CallWindowProcW(original, hWnd, message, wParam, lParam);
}

BOOL CALLBACK FindCurrentProcessMainWindow(HWND hWnd, LPARAM parameter)
{
    DWORD processID = 0;
    GetWindowThreadProcessId(hWnd, &processID);
    if (processID != GetCurrentProcessId() || !IsWindowVisible(hWnd) ||
        GetWindow(hWnd, GW_OWNER) != nullptr) {
        return TRUE;
    }
    *reinterpret_cast<HWND*>(parameter) = hWnd;
    return FALSE;
}

bool InstallContractUseWindowProc()
{
    constexpr int kWaitLimitMs = 120000;
    int waitedMs = 0;
    HWND window = nullptr;
    while (!window && waitedMs < kWaitLimitMs) {
        EnumWindows(&FindCurrentProcessMainWindow,
            reinterpret_cast<LPARAM>(&window));
        if (window) break;
        Sleep(250);
        waitedMs += 250;
    }
    if (!window) {
        LogLine("contract-use main window unavailable after wait_ms=%d",
            waitedMs);
        return false;
    }

    WNDPROC original = reinterpret_cast<WNDPROC>(
        GetWindowLongPtrW(window, GWLP_WNDPROC));
    if (!original) {
        LogLine("contract-use main window has no window procedure hwnd=%p",
            window);
        return false;
    }
    g_originalDnfWindowProc = original;
    SetLastError(ERROR_SUCCESS);
    LONG_PTR previous = SetWindowLongPtrW(
        window, GWLP_WNDPROC,
        reinterpret_cast<LONG_PTR>(&ProxyDnfWindowProc));
    DWORD error = GetLastError();
    if (!previous && error != ERROR_SUCCESS) {
        g_originalDnfWindowProc = nullptr;
        LogLine("contract-use window procedure install failed hwnd=%p error=%lu",
            window, error);
        return false;
    }
    g_dnfWindow = window;
    g_originalDnfWindowProc = reinterpret_cast<WNDPROC>(previous);
    InterlockedExchangePointer(&g_luaNoticeBridgeWindow, window);

    UINT noticeMessage = RegisterWindowMessageW(kLuaNoticeWindowMessageName);
    if (noticeMessage != 0) {
        InterlockedExchange(&g_luaNoticeWindowMessage,
            static_cast<LONG>(noticeMessage));
        DWORD windowProcessID = 0;
        DWORD windowThreadID = GetWindowThreadProcessId(
            window, &windowProcessID);
        LogLine("lua client notice bridge ready hwnd=%p message=0x%04X ui_tid=%lu pid=%lu renderer=owned-popup-overlay",
            window, noticeMessage, windowThreadID,
            windowProcessID);
        if (InterlockedCompareExchange(
                &g_luaUiReadyEventQueued, 1, 0) == 0 &&
            !QueueLuaClientEventInternal(
                DNF90_CLIENT_EVENT_UI_READY, windowThreadID)) {
            InterlockedExchange(&g_luaUiReadyEventQueued, 0);
        }
    } else {
        LogLine("lua client notice bridge unavailable message=0x%04X error=%lu",
            noticeMessage,
            GetLastError());
    }

    UINT combatPanelMessage = RegisterWindowMessageW(
        kCombatPanelWindowMessageName);
    if (combatPanelMessage != 0) {
        InterlockedExchange(&g_combatPanelWindowMessage,
            static_cast<LONG>(combatPanelMessage));
        UINT_PTR timer = SetTimer(window, kCombatPanelPollTimer,
            kCombatPanelPollIntervalMs, nullptr);
        LogLine("combat-panel UI bridge ready hwnd=%p message=0x%04X "
            "timer=%p interval_ms=%u owner=%u is_open=+0x%08X",
            window, combatPanelMessage,
            reinterpret_cast<void*>(timer), kCombatPanelPollIntervalMs,
            static_cast<unsigned int>(kPersonalInfoUiOwner),
            static_cast<unsigned int>(kSceneUiIsOpenRva));
    } else {
        LogLine("combat-panel UI bridge unavailable error=%lu",
            GetLastError());
    }
    LogLine("contract-use window procedure installed hwnd=%p original=%p wait_ms=%d",
        window, g_originalDnfWindowProc, waitedMs);
    return g_originalDnfWindowProc != nullptr;
}

int __fastcall ProxyCurrentItemRowParse(int itemObject, int unused, char* raw)
{
    unsigned char rawCopy[kCurrentItemRawBytes] = { 0 };
    bool captured = TryCopyTraceBytes(raw, rawCopy, sizeof(rawCopy));
    int result = g_originalCurrentItemRowParse
        ? g_originalCurrentItemRowParse(itemObject, unused, raw)
        : 0;
    if (captured) CacheCurrentItemRow(static_cast<uintptr_t>(itemObject), rawCopy);
    return result;
}

int __fastcall ProxyInventoryUseUIA(
    void* self, void* /*unused*/, int eventCode, int eventDetail, int arg2, int arg3)
{
    InventoryUseUIAFn original =
        reinterpret_cast<InventoryUseUIAFn>(g_originalInventoryUseUIA);
    if (!original) return 0;
    if (g_contractUseFallbackActive ||
        eventCode != 0x12C || eventDetail != 0x0D) {
        return original(self, eventCode, eventDetail, arg2, arg3);
    }

    LONG hit = InterlockedIncrement(&g_contractUseUIAHitCount);
    CurrentContractSelection selection = {};
    bool contractSelected = ResolveCurrentPremiumContractSelection(&selection);
    LONG before = InterlockedCompareExchange(&g_clientOp44SendCount, 0, 0);
    LogLine("contract-use callback A hit=%ld selected=%d slot=%d item=%lu identity=%lu before=%ld",
        hit, contractSelected ? 1 : 0, selection.slot,
        static_cast<unsigned long>(selection.templateID),
        static_cast<unsigned long>(selection.identity), before);
    g_contractUseFallbackActive = true;
    int result = original(self, eventCode, eventDetail, arg2, arg3);
    LONG after = InterlockedCompareExchange(&g_clientOp44SendCount, 0, 0);
    if (contractSelected && after == before) {
        SendCurrentPremiumContractUse(selection);
    }
    g_contractUseFallbackActive = false;
    return result;
}

void __cdecl ProxyInventoryUseUIB()
{
    InventoryUseUIBFn original =
        reinterpret_cast<InventoryUseUIBFn>(g_originalInventoryUseUIB);
    if (!original) return;
    if (g_contractUseFallbackActive) {
        original();
        return;
    }

    LONG hit = InterlockedIncrement(&g_contractUseUIBHitCount);
    CurrentContractSelection selection = {};
    bool contractSelected = ResolveCurrentPremiumContractSelection(&selection);
    LONG before = InterlockedCompareExchange(&g_clientOp44SendCount, 0, 0);
    LogLine("contract-use callback B hit=%ld selected=%d slot=%d item=%lu identity=%lu before=%ld",
        hit, contractSelected ? 1 : 0, selection.slot,
        static_cast<unsigned long>(selection.templateID),
        static_cast<unsigned long>(selection.identity), before);
    g_contractUseFallbackActive = true;
    original();
    LONG after = InterlockedCompareExchange(&g_clientOp44SendCount, 0, 0);
    if (contractSelected && after == before) {
        SendCurrentPremiumContractUse(selection);
    }
    g_contractUseFallbackActive = false;
}

uintptr_t RunInventoryUseGateFallback(
    InventoryUseGateFn original, volatile LONG* hitCounter, const char* label,
    void* self, int arg0, int arg1, int arg2)
{
    if (!original) return 0;
    if (g_contractUseFallbackActive) {
        return original(self, arg0, arg1, arg2);
    }

    LONG hit = InterlockedIncrement(hitCounter);
    CurrentContractSelection selection = {};
    bool contractSelected = ResolveCurrentPremiumContractSelection(&selection);
    LONG before = InterlockedCompareExchange(&g_clientOp44SendCount, 0, 0);
    LogLine("contract-use %s hit=%ld selected=%d slot=%d item=%lu identity=%lu before=%ld args=%08X/%08X/%08X",
        label, hit, contractSelected ? 1 : 0, selection.slot,
        static_cast<unsigned long>(selection.templateID),
        static_cast<unsigned long>(selection.identity), before,
        static_cast<unsigned int>(arg0), static_cast<unsigned int>(arg1),
        static_cast<unsigned int>(arg2));

    // The current client can reject a contract in sub_275A140's native 0x7D4
    // eligibility check before any of the three lower op44 callbacks runs.
    // Keep the whole native callback first; only recover an allow-listed,
    // selected main-bag contract when that callback emitted no op44.
    g_contractUseFallbackActive = true;
    uintptr_t result = original(self, arg0, arg1, arg2);
    if (!contractSelected) {
        contractSelected = ResolveCurrentPremiumContractSelection(&selection);
    }
    LONG after = InterlockedCompareExchange(&g_clientOp44SendCount, 0, 0);
    if (contractSelected && after == before) {
        SendCurrentPremiumContractUse(selection);
    }
    g_contractUseFallbackActive = false;
    return result;
}

uintptr_t __fastcall ProxyInventoryUseGateA(
    void* self, void* /*unused*/, int arg0, int arg1, int arg2)
{
    return RunInventoryUseGateFallback(
        reinterpret_cast<InventoryUseGateFn>(g_originalInventoryUseGateA),
        &g_contractUseGateAHitCount, "gate-A", self, arg0, arg1, arg2);
}

uintptr_t __fastcall ProxyInventoryUseGateB(
    void* self, void* /*unused*/, int arg0, int arg1, int arg2)
{
    return RunInventoryUseGateFallback(
        reinterpret_cast<InventoryUseGateFn>(g_originalInventoryUseGateB),
        &g_contractUseGateBHitCount, "gate-B", self, arg0, arg1, arg2);
}

unsigned char __fastcall ProxyInventoryUsePanel(
    void* self, void* /*unused*/, uint32_t templateID, int slot)
{
    InventoryUsePanelFn original =
        reinterpret_cast<InventoryUsePanelFn>(g_originalInventoryUsePanel);
    if (!original) return 0;
    if (g_contractUseFallbackActive) {
        return original(self, templateID, slot);
    }

    LONG hit = InterlockedIncrement(&g_contractUsePanelHitCount);
    CurrentContractSelection selection = {};
    bool contractSelected = ResolveCurrentPremiumContractPanelSelection(
        templateID, slot, &selection);
    LONG before = InterlockedCompareExchange(&g_clientOp44SendCount, 0, 0);
    LogLine("contract-use panel hit=%ld selected=%d slot=%d item=%lu identity=%lu before=%ld",
        hit, contractSelected ? 1 : 0, selection.slot,
        static_cast<unsigned long>(selection.templateID),
        static_cast<unsigned long>(selection.identity), before);

    // Preserve the native panel behavior first.  The fallback only covers its
    // audited global-gate branch when no C2S op44 reached the packet logger.
    g_contractUseFallbackActive = true;
    unsigned char result = original(self, templateID, slot);
    LONG after = InterlockedCompareExchange(&g_clientOp44SendCount, 0, 0);
    if (contractSelected && after == before) {
        SendCurrentPremiumContractUse(selection);
    }
    g_contractUseFallbackActive = false;
    return result;
}

void LogSocketPanelSelection(int selectedItem)
{
    LONG selection = InterlockedIncrement(&g_socketTraceSelectionCount);
    if (selection > kSocketTraceMaxSelections) return;

    // Current NoPack's selected item wrapper uses vtable+0x28
    // (sub_105FB00) to return this+8.  Its +0x18 field is the exact u32
    // template value consumed by sub_2112D50.  sub_21792A0 itself receives
    // a stack-local parser object, so its pointer cannot be compared to this
    // persistent selected wrapper after scene-row parsing has returned.
    uintptr_t itemData = static_cast<uintptr_t>(selectedItem) + 8;
    uint32_t templateID = 0;
    bool templateReadable = TryCopyTraceBytes(reinterpret_cast<const void*>(itemData + 0x18),
        &templateID, sizeof(templateID));
    CurrentItemRowTrace matches[kSocketTraceTemplateMatches] = {};
    LONG matchCount = templateReadable
        ? FindCurrentItemRowsByTemplate(templateID, matches, kSocketTraceTemplateMatches)
        : 0;
    unsigned char objectBytes[0xC4] = { 0 };
    bool objectReadable = TryCopyTraceBytes(reinterpret_cast<const void*>(itemData),
        objectBytes, sizeof(objectBytes));
    char objectHex[640] = { 0 };
    if (objectReadable) FormatTraceBytes(objectBytes, sizeof(objectBytes), objectHex, sizeof(objectHex));

    // The selected pointer itself is the polymorphic wrapper consumed by
    // sub_2112F70.  Record its vtable and only the two narrow ranges needed to
    // map the lock renderer back to the current raw row.  This is passive
    // evidence collection: no wrapper, vtable, packet or item byte is changed.
    uintptr_t selectedVtable = 0;
    bool selectedVtableReadable = TryCopyTraceBytes(reinterpret_cast<const void*>(selectedItem),
        &selectedVtable, sizeof(selectedVtable));
    unsigned char wrapperHead[0x40] = { 0 };
    bool wrapperHeadReadable = TryCopyTraceBytes(reinterpret_cast<const void*>(selectedItem),
        wrapperHead, sizeof(wrapperHead));
    char wrapperHeadHex[256] = { 0 };
    if (wrapperHeadReadable) {
        FormatTraceBytes(wrapperHead, sizeof(wrapperHead), wrapperHeadHex, sizeof(wrapperHeadHex));
    }
    unsigned char wrapperState[0x120] = { 0 };
    bool wrapperStateReadable = TryCopyTraceBytes(
        reinterpret_cast<const void*>(static_cast<uintptr_t>(selectedItem) + 0x2E0),
        wrapperState, sizeof(wrapperState));
    char wrapperStateHex[896] = { 0 };
    if (wrapperStateReadable) {
        FormatTraceBytes(wrapperState, sizeof(wrapperState), wrapperStateHex, sizeof(wrapperStateHex));
    }
    unsigned char vtableBytes[0xE0] = { 0 };
    bool vtableReadable = selectedVtableReadable && TryCopyTraceBytes(
        reinterpret_cast<const void*>(selectedVtable), vtableBytes, sizeof(vtableBytes));
    char vtableHex[704] = { 0 };
    if (vtableReadable) {
        FormatTraceBytes(vtableBytes, sizeof(vtableBytes), vtableHex, sizeof(vtableHex));
    }

    LogLine("socket-trace panel-select ordinal=%ld item_wrapper=0x%08X item_data=0x%08X template_read=%d template_id=%lu raw_template_matches=%ld cached_rows=%ld",
        selection, selectedItem, itemData, templateReadable ? 1 : 0,
        static_cast<unsigned long>(templateID), matchCount, g_socketTraceRowSequence);
    for (LONG i = 0; i < matchCount; ++i) {
        char rawHex[384] = { 0 };
        FormatTraceBytes(matches[i].raw, sizeof(matches[i].raw), rawHex, sizeof(rawHex));
        LogLine("socket-trace raw-template-match ordinal=%ld match=%ld parse_sequence=%ld parser_object=0x%08X raw_slot=%u raw_template_id=%lu raw77=%s",
            selection, i + 1, matches[i].sequence, matches[i].parserObject,
            CurrentItemRowSlot(matches[i].raw),
            static_cast<unsigned long>(CurrentItemRowTemplateID(matches[i].raw)), rawHex);
    }
    LogLine("socket-trace panel-object ordinal=%ld item_data=0x%08X bytes_c4=%s",
        selection, itemData, objectReadable ? objectHex : "<read-failed>");
    LogLine("socket-trace selected-wrapper ordinal=%ld wrapper=0x%08X vtable_read=%d vtable=0x%08X head_40=%s",
        selection, selectedItem, selectedVtableReadable ? 1 : 0, selectedVtable,
        wrapperHeadReadable ? wrapperHeadHex : "<read-failed>");
    LogLine("socket-trace selected-wrapper-state ordinal=%ld wrapper=0x%08X range=+0x2E0..+0x3FF bytes_120=%s",
        selection, selectedItem, wrapperStateReadable ? wrapperStateHex : "<read-failed>");
    LogLine("socket-trace selected-vtable ordinal=%ld vtable=0x%08X bytes_e0=%s",
        selection, selectedVtable, vtableReadable ? vtableHex : "<read-failed>");
}

char __fastcall ProxySocketPanelSelect(void* self, void* /*unused*/, int selectedItem, int arg)
{
    char result = g_originalSocketPanelSelect
        ? g_originalSocketPanelSelect(self, selectedItem, arg)
        : 0;
    LogSocketPanelSelection(selectedItem);
    return result;
}

char __fastcall ProxySocketOpenWriter(void* self, void* /*unused*/)
{
    LONG hit = InterlockedIncrement(&g_socketTraceWriterCount);
    uintptr_t selectedItem = 0;
    bool selectedReadable = self && TryCopyTraceBytes(
        reinterpret_cast<const void*>(reinterpret_cast<uintptr_t>(self) + 0x1B4),
        &selectedItem, sizeof(selectedItem));
    if (hit <= kSocketTraceMaxWriterHits) {
        LogLine("socket-trace writer-enter ordinal=%ld ui=0x%08X selected_read=%d selected_wrapper=0x%08X",
            hit, self, selectedReadable ? 1 : 0, selectedItem);
    }

    char result = g_originalSocketOpenWriter ? g_originalSocketOpenWriter(self) : 0;
    if (hit <= kSocketTraceMaxWriterHits) {
        LogLine("socket-trace writer-return ordinal=%ld result=%d", hit,
            static_cast<int>(static_cast<unsigned char>(result)));
    }
    return result;
}

bool __cdecl ProxySocketOp14ExtensionGate(int templateID)
{
    bool accepted = g_originalSocketOp14ExtensionGate
        ? g_originalSocketOp14ExtensionGate(templateID)
        : false;
    // Do not turn a broad scene-reader hook into a logging hot path.  The
    // passive socket trace already established this selected normal-equipment
    // template; record only its actual current-client gate result.
    if (static_cast<uint32_t>(templateID) == kSocketTraceSelectedTemplateID) {
        LONG hit = InterlockedIncrement(&g_socketTraceExtensionGateCount);
        if (hit <= kSocketTraceMaxExtensionGateHits) {
            LogLine("socket-trace op14-list3-extension-gate ordinal=%ld template_id=%lu accepted=%d",
                hit, static_cast<unsigned long>(static_cast<uint32_t>(templateID)),
                accepted ? 1 : 0);
        }
    }
    return accepted;
}

void LogCodecSetKey(int index, void* self, int keyPtr, int keyLength, int result, void* returnAddress)
{
    char hex[360] = { 0 };
    FormatHexBytes(reinterpret_cast<const void*>(keyPtr), keyLength, hex, sizeof(hex));

    uintptr_t vtable = 0;
    uintptr_t state = 0;
    __try {
        vtable = self ? *reinterpret_cast<uintptr_t*>(self) : 0;
        state = self ? *(reinterpret_cast<uintptr_t*>(self) + 2) : 0;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        vtable = 0;
        state = 0;
    }

    LogLine("codec-setkey idx=%d self=%p vt=0x%08X state=0x%08X key=%p len=%d ret=0x%08X caller=0x%08X bytes=%s",
        index, self, vtable, state, reinterpret_cast<void*>(keyPtr), keyLength,
        result, reinterpret_cast<uintptr_t>(returnAddress), hex);
}

bool IsTclsTrimSpace(wchar_t value)
{
    return value == L' ' || value == L'\t' || value == L'\r' || value == L'\n';
}

bool WideSliceEquals(const wchar_t* text, size_t length, const wchar_t* expected)
{
    if (!text || !expected) return false;
    size_t expectedLength = wcslen(expected);
    return length == expectedLength && wmemcmp(text, expected, length) == 0;
}

// The launch parser owns a vector<wchar_t*> at +0xC4/+0xC8.  Its entries are
// direct NUL-terminated strings.  The fetch output objects are a different
// type and are written only through the current EXE's native assign method.
bool TryReadTclsSlot(unsigned int slot, const wchar_t** text, size_t* length)
{
    if (!text || !length) return false;
    *text = nullptr;
    *length = 0;

    __try {
        unsigned char* parser = static_cast<unsigned char*>(g_currentTclsParser);
        if (!parser) return false;

        void** begin = *reinterpret_cast<void***>(parser + 0xC4);
        void** end = *reinterpret_cast<void***>(parser + 0xC8);
        if (!begin || !end || end < begin) return false;

        size_t count = static_cast<size_t>(end - begin);
        if (count == 0 || count > 64 || slot >= count) return false;

        const wchar_t* valueText = reinterpret_cast<const wchar_t*>(begin[slot]);
        if (!valueText) return false;

        size_t valueLength = 0;
        while (valueLength < 4096 && valueText[valueLength] != L'\0') ++valueLength;
        if (valueLength == 0 || valueLength == 4096) return false;

        size_t first = 0;
        size_t last = valueLength;
        while (first < last && IsTclsTrimSpace(valueText[first])) ++first;
        while (last > first && IsTclsTrimSpace(valueText[last - 1])) --last;
        if (last == first) return false;

        valueText += first;
        valueLength = last - first;
        if (WideSliceEquals(valueText, valueLength, L"<bad-wide>") ||
            WideSliceEquals(valueText, valueLength, L"<null>")) {
            return false;
        }

        *text = valueText;
        *length = valueLength;
        return true;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        return false;
    }
}

bool IsDirectTclsLaunch()
{
    const wchar_t* marker = nullptr;
    size_t markerLength = 0;
    return TryReadTclsSlot(0, &marker, &markerLength) &&
        WideSliceEquals(marker, markerLength, L"99");
}

bool AssignTclsSlot(void* output, unsigned int slot)
{
    if (!output || !g_gameWideAssign || !IsDirectTclsLaunch()) return false;

    const wchar_t* value = nullptr;
    size_t valueLength = 0;
    if (!TryReadTclsSlot(slot, &value, &valueLength)) return false;

    __try {
        g_gameWideAssign(output, value, valueLength);
        LogLine("tcls native fallback slot=%u len=%u", slot, static_cast<unsigned int>(valueLength));
        return true;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("tcls native fallback exception slot=%u code=0x%08X", slot, GetExceptionCode());
        return false;
    }
}

size_t ReadGameWideLength(void* value)
{
    if (!value) return 0;
    __try {
        return *reinterpret_cast<unsigned int*>(static_cast<unsigned char*>(value) + 0x14);
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        return 0;
    }
}

bool __fastcall ProxyTclsParse(void* self, void* /*unused*/, const wchar_t* text)
{
    g_currentTclsParser = self;
    bool result = g_originalTclsParse ? g_originalTclsParse(self, text) : false;
    g_currentTclsParser = nullptr;
    LogLine("tcls parse result=%d", result ? 1 : 0);
    return result;
}

bool __fastcall ProxyTclsFetchLogin(void* self, void* /*unused*/, void* outIp, void* outPort, void* outChannel)
{
    bool result = g_originalTclsFetchLogin
        ? g_originalTclsFetchLogin(self, outIp, outPort, outChannel)
        : false;
    if (!result) {
        // Match the native compatibility behavior: attempt all three writes
        // even if an earlier field fails, then report aggregate success.
        bool ipAssigned = AssignTclsSlot(outIp, 1);
        bool portAssigned = AssignTclsSlot(outPort, 2);
        bool channelAssigned = AssignTclsSlot(outChannel, 5);
        result = ipAssigned && portAssigned && channelAssigned;
    }
    LogLine("tcls fetch login result=%d", result ? 1 : 0);
    return result;
}

bool __fastcall ProxyTclsFetchText(void* self, void* /*unused*/, int key, void* output)
{
    bool result = g_originalTclsFetchText ? g_originalTclsFetchText(self, key, output) : false;
    if (!result && key == 2) result = AssignTclsSlot(output, 3);
    LogLine("tcls fetch text key=%d result=%d", key, result ? 1 : 0);
    return result;
}

bool __fastcall ProxyTclsFetchCrypto(void* self, void* /*unused*/, void* output)
{
    bool result = g_originalTclsFetchCrypto ? g_originalTclsFetchCrypto(self, output) : false;
    if (!result) result = AssignTclsSlot(output, 4);
    LogLine("tcls fetch crypto result=%d", result ? 1 : 0);
    return result;
}

bool __fastcall ProxyTclsFetchTail(void* self, void* /*unused*/, void* output)
{
    bool result = g_originalTclsFetchTail ? g_originalTclsFetchTail(self, output) : false;
    if (!result) result = AssignTclsSlot(output, 15);
    LogLine("tcls fetch tail result=%d", result ? 1 : 0);
    return result;
}

bool __stdcall ProxyTclsTail(void* output)
{
    bool result = g_originalTclsTail ? g_originalTclsTail(output) : false;
    if (!result || ReadGameWideLength(output) == 0) {
        if (AssignTclsSlot(output, 12)) result = true;
    }
    LogLine("tcls tail result=%d len=%u", result ? 1 : 0,
        static_cast<unsigned int>(ReadGameWideLength(output)));
    return result;
}

int __fastcall ProxyCodecSetKey0(void* self, void* /*unused*/, int keyPtr, int keyLength)
{
    int result = g_originalCodecSetKey0
        ? g_originalCodecSetKey0(self, keyPtr, keyLength)
        : 1879048192;
    LogCodecSetKey(0, self, keyPtr, keyLength, result, _ReturnAddress());
    return result;
}

int __fastcall ProxyCodecSetKey2(void* self, void* /*unused*/, int keyPtr, int keyLength)
{
    int result = g_originalCodecSetKey2
        ? g_originalCodecSetKey2(self, keyPtr, keyLength)
        : 1879048192;
    LogCodecSetKey(2, self, keyPtr, keyLength, result, _ReturnAddress());
    return result;
}

int __fastcall ProxyCodecSetKey3(void* self, void* /*unused*/, int keyPtr, int keyLength)
{
    int result = g_originalCodecSetKey3
        ? g_originalCodecSetKey3(self, keyPtr, keyLength)
        : 1879048192;
    LogCodecSetKey(3, self, keyPtr, keyLength, result, _ReturnAddress());
    return result;
}

int __fastcall ProxyCodecSetKey7(void* self, void* /*unused*/, int keyPtr, int keyLength)
{
    int result = g_originalCodecSetKey7
        ? g_originalCodecSetKey7(self, keyPtr, keyLength)
        : 1879048192;
    LogCodecSetKey(7, self, keyPtr, keyLength, result, _ReturnAddress());
    return result;
}

int __fastcall ProxyCodecSetKey8(void* self, void* /*unused*/, int keyPtr, int keyLength)
{
    int result = g_originalCodecSetKey8
        ? g_originalCodecSetKey8(self, keyPtr, keyLength)
        : 1879048192;
    LogCodecSetKey(8, self, keyPtr, keyLength, result, _ReturnAddress());
    return result;
}

bool InstallKnownInlineHook(uintptr_t target, const unsigned char* expected, size_t patchLength,
    void* detour, void** original, const char* label)
{
    if (!target || !expected || patchLength < 5 || !detour || !original) return false;
    if (!BytesMatch(reinterpret_cast<unsigned char*>(target), expected, patchLength)) {
        LogLine("%s native prologue mismatch at 0x%08X", label, target);
        return false;
    }

    unsigned char* trampoline = static_cast<unsigned char*>(VirtualAlloc(nullptr, patchLength + 5,
        MEM_COMMIT | MEM_RESERVE, PAGE_EXECUTE_READWRITE));
    if (!trampoline) {
        LogLine("%s trampoline allocation failed: %u", label, GetLastError());
        return false;
    }

    memcpy(trampoline, reinterpret_cast<void*>(target), patchLength);
    trampoline[patchLength] = 0xE9;
    *reinterpret_cast<int32_t*>(trampoline + patchLength + 1) = static_cast<int32_t>(
        static_cast<intptr_t>(target + patchLength) -
        reinterpret_cast<intptr_t>(trampoline + patchLength + 5));

    DWORD oldProtection = 0;
    if (!VirtualProtect(reinterpret_cast<void*>(target), patchLength, PAGE_EXECUTE_READWRITE, &oldProtection)) {
        LogLine("%s VirtualProtect failed: %u", label, GetLastError());
        VirtualFree(trampoline, 0, MEM_RELEASE);
        return false;
    }

    unsigned char jump[5] = { 0xE9 };
    *reinterpret_cast<int32_t*>(jump + 1) = static_cast<int32_t>(
        reinterpret_cast<intptr_t>(detour) - static_cast<intptr_t>(target + 5));
    memcpy(reinterpret_cast<void*>(target), jump, sizeof(jump));
    for (size_t i = sizeof(jump); i < patchLength; ++i) {
        *reinterpret_cast<unsigned char*>(target + i) = 0x90;
    }

    DWORD ignoredProtection = 0;
    VirtualProtect(reinterpret_cast<void*>(target), patchLength, oldProtection, &ignoredProtection);
    FlushInstructionCache(GetCurrentProcess(), reinterpret_cast<void*>(target), patchLength);
    FlushInstructionCache(GetCurrentProcess(), trampoline, patchLength + 5);
    *original = trampoline;
    LogLine("%s installed target=0x%08X trampoline=%p", label, target, trampoline);
    return true;
}

bool InstallPartyDirectoryFullPageCompatibility()
{
    // sub_326D450 opens owner 0x17B for ordinary channels. That owner is the
    // six-row regional summary and never sends the full-directory request.
    // The current EXE's native full party directory is owner 9 and its only
    // native open site passes owner 9. Keep the controller lifecycle symmetric
    // by changing its owner guard, open owner, and both owner-0x17B close paths
    // as one validated patch set. Preserve the controller's existing mode/group
    // arguments; owner 9's current-EXE open handler does not read either value.
    static const unsigned char kOwner9[] = {
        0x68, 0x09, 0x00, 0x00, 0x00
    };
    static const unsigned char kGuardContext[] = {
        0x55, 0x8B, 0xEC, 0x57, 0x8B, 0xF9, 0x8B, 0x0D,
        0x64, 0x27, 0x1B, 0x05, 0x68, 0x7B, 0x01, 0x00,
        0x00, 0xE8, 0xDA, 0x7A, 0xD6, 0xFF, 0x84, 0xC0,
        0x75, 0x46, 0x83, 0x3D, 0xE4, 0x0A, 0x2E, 0x05,
        0x00, 0x75, 0x3D, 0x56, 0x6A, 0x00, 0x6A, 0xFF,
        0x6A, 0xFF, 0x6A, 0x00, 0x6A
    };
    static const unsigned char kOpenContext[] = {
        0x8B, 0x75, 0x08, 0x8B, 0x0D, 0x64, 0x27, 0x1B,
        0x05, 0x83, 0xC4, 0x1C, 0x56, 0x6A, 0x00, 0x68,
        0x7B, 0x01, 0x00, 0x00, 0xE8, 0x24, 0x02, 0xD7,
        0xFF, 0x89, 0x77, 0x34
    };
    static const unsigned char kCloseContextA[] = {
        0x8B, 0x0D, 0x64, 0x27, 0x1B, 0x05, 0x6A, 0x00,
        0x6A, 0xFF, 0x68, 0x7C, 0x01, 0x00, 0x00, 0xE8,
        0xA5, 0xB1, 0xD6, 0xFF, 0x8B, 0x0D, 0x64, 0x27,
        0x1B, 0x05, 0x6A, 0x00, 0x6A, 0xFF, 0x68, 0x7B,
        0x01, 0x00, 0x00, 0xE8, 0x91, 0xB1, 0xD6, 0xFF,
        0x5F
    };
    static const unsigned char kCloseContextB[] = {
        0x8B, 0x0D, 0x64, 0x27, 0x1B, 0x05, 0x6A, 0x00,
        0x6A, 0xFF, 0x68, 0x7C, 0x01, 0x00, 0x00, 0xE8,
        0x29, 0xB1, 0xD6, 0xFF, 0x8B, 0x0D, 0x64, 0x27,
        0x1B, 0x05, 0x6A, 0x00, 0x6A, 0xFF, 0x68, 0x7B,
        0x01, 0x00, 0x00, 0xE8, 0x15, 0xB1, 0xD6, 0xFF,
        0x5E
    };
    struct Context {
        uintptr_t target;
        const unsigned char* expected;
        size_t length;
        const char* label;
    };
    Context contexts[] = {
        {
            g_dnfBase + 0x02E6CF20,
            kGuardContext, sizeof(kGuardContext), "owner guard context"
        },
        {
            g_dnfBase + 0x02E6CF63,
            kOpenContext, sizeof(kOpenContext), "open context"
        },
        {
            g_dnfBase + 0x02E6D417,
            kCloseContextA, sizeof(kCloseContextA), "close context A"
        },
        {
            g_dnfBase + 0x02E6D493,
            kCloseContextB, sizeof(kCloseContextB), "close context B"
        },
    };
    struct Patch {
        uintptr_t target;
        const unsigned char* replacement;
        size_t length;
    };
    Patch patches[] = {
        {
            g_dnfBase + kPartyDirectoryOwnerGuardRva,
            kOwner9, sizeof(kOwner9)
        },
        {
            g_dnfBase + kPartyDirectoryOpenOwnerRva,
            kOwner9, sizeof(kOwner9)
        },
        {
            g_dnfBase + kPartyDirectoryCloseOwnerARva,
            kOwner9, sizeof(kOwner9)
        },
        {
            g_dnfBase + kPartyDirectoryCloseOwnerBRva,
            kOwner9, sizeof(kOwner9)
        },
    };

    for (size_t i = 0; i < _countof(contexts); ++i) {
        if (!BytesMatch(reinterpret_cast<unsigned char*>(contexts[i].target),
                contexts[i].expected, contexts[i].length)) {
            LogLine("party-directory full-page compatibility rejected: %s "
                "bytes mismatch at 0x%08X",
                contexts[i].label, contexts[i].target);
            return false;
        }
    }

    uintptr_t patchStart = patches[0].target;
    uintptr_t patchEnd =
        patches[_countof(patches) - 1].target +
        patches[_countof(patches) - 1].length;
    DWORD oldProtection = 0;
    if (!VirtualProtect(reinterpret_cast<void*>(patchStart),
            patchEnd - patchStart, PAGE_EXECUTE_READWRITE, &oldProtection)) {
        LogLine("party-directory full-page compatibility VirtualProtect failed: %u",
            GetLastError());
        return false;
    }
    for (size_t i = 0; i < _countof(patches); ++i) {
        memcpy(reinterpret_cast<void*>(patches[i].target),
            patches[i].replacement, patches[i].length);
    }
    DWORD ignoredProtection = 0;
    VirtualProtect(reinterpret_cast<void*>(patchStart),
        patchEnd - patchStart, oldProtection, &ignoredProtection);
    FlushInstructionCache(GetCurrentProcess(),
        reinterpret_cast<void*>(patchStart), patchEnd - patchStart);
    LogLine("party-directory full-page compatibility installed "
        "guard=0x%08X open=0x%08X close_a=0x%08X close_b=0x%08X",
        patches[0].target, patches[1].target,
        patches[2].target, patches[3].target);
    return true;
}

bool __fastcall ProxyChannelScriptDownload(void* self, void* /*unused*/)
{
    LogLine("channel script download enter self=%p", self);
    bool result = g_originalChannelScriptDownload
        ? g_originalChannelScriptDownload(self)
        : false;
    LogLine("channel script download return result=%d", result ? 1 : 0);
    return result;
}

bool __fastcall ProxyChannelScriptLoad(void* self, void* /*unused*/, const char* text)
{
    size_t length = 0;
    char hex[360] = { 0 };
    __try {
        if (text) {
            while (length < 0x80000 && text[length] != '\0') ++length;
            FormatHexBytes(text, static_cast<int>(length), hex, sizeof(hex));
        }
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        length = 0;
        _snprintf(hex, sizeof(hex) - 1, "<read-exception-0x%08X>", GetExceptionCode());
    }
    LogLine("channel script load enter self=%p len=%u bytes=%s",
        self, static_cast<unsigned int>(length), hex);
    bool result = g_originalChannelScriptLoad
        ? g_originalChannelScriptLoad(self, text)
        : false;
    LogLine("channel script load return result=%d", result ? 1 : 0);
    return result;
}

void* __fastcall ProxyChannelRuntimeLoad(void* self, void* /*unused*/, const void* key)
{
    unsigned int serverID = 0;
    unsigned int channelID = 0;
    __try {
        if (key) {
            const unsigned int* values = static_cast<const unsigned int*>(key);
            serverID = values[0];
            channelID = values[1];
        }
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        serverID = 0xFFFFFFFFu;
        channelID = 0xFFFFFFFFu;
    }

    void* scriptRecord = nullptr;
    unsigned int scriptType = 0xFFFFFFFFu;
    __try {
        if (serverID != 0xFFFFFFFFu && channelID != 0xFFFFFFFFu) {
            ChannelScriptLookupFn lookup = reinterpret_cast<ChannelScriptLookupFn>(
                g_dnfBase + kChannelScriptLookupRva);
            scriptRecord = lookup(reinterpret_cast<void*>(g_dnfBase + kChannelScriptStoreRva),
                static_cast<int>(serverID), static_cast<int>(channelID));
            if (scriptRecord) {
                scriptType = *reinterpret_cast<unsigned int*>(
                    static_cast<unsigned char*>(scriptRecord) + 0x20);
            }
        }
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        scriptRecord = nullptr;
        scriptType = 0xFFFFFFFFu;
    }

    void* result = g_originalChannelRuntimeLoad
        ? g_originalChannelRuntimeLoad(self, key)
        : nullptr;
    LogLine("channel runtime load server=%u channel=%u script=%p type=%u result=%p",
        serverID, channelID, scriptRecord, scriptType, result);
    return result;
}

int __stdcall ProxyChannelCategoryInsert(int category, void* channel)
{
    unsigned int serverID = 0xFFFFFFFFu;
    unsigned int channelID = 0xFFFFFFFFu;
    unsigned int channelType = 0xFFFFFFFFu;
    __try {
        if (channel) {
            const unsigned int* fields = static_cast<const unsigned int*>(channel);
            serverID = fields[1];
            channelType = fields[2];
            channelID = fields[0x70 / sizeof(unsigned int)];
        }
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
    }
    LogLine("channel category insert category=%d server=%u channel=%u type=%u object=%p",
        category, serverID, channelID, channelType, channel);
    return g_originalChannelCategoryInsert
        ? g_originalChannelCategoryInsert(category, channel)
        : 0;
}

bool StoreNativeHudState(uintptr_t descriptorRva, uintptr_t valueRva,
    uintptr_t lockRva, uintptr_t storeRva, int value)
{
    AtomicSpinFn lock = reinterpret_cast<AtomicSpinFn>(
        g_dnfBase + kAtomicSpinLockRva);
    AtomicSpinFn unlock = reinterpret_cast<AtomicSpinFn>(
        g_dnfBase + kAtomicSpinUnlockRva);
    ObfuscatedStoreFn store = reinterpret_cast<ObfuscatedStoreFn>(
        g_dnfBase + storeRva);
    void* descriptor = reinterpret_cast<void*>(g_dnfBase + descriptorRva);
    int* encryptedValue = reinterpret_cast<int*>(g_dnfBase + valueRva);
    void* stateLock = reinterpret_cast<void*>(g_dnfBase + lockRva);

    bool stored = false;
    lock(stateLock);
    __try {
        store(descriptor, &value, encryptedValue);
        stored = true;
    }
    __finally {
        unlock(stateLock);
    }
    return stored;
}

bool CommitNativeHudChannelState(int serverID, int channelID)
{
    if (serverID < 0 || channelID <= 0) {
        return false;
    }

    bool serverStored = StoreNativeHudState(
        kHudServerIndexDescriptorRva, kHudServerIndexValueRva,
        kHudServerIndexLockRva, kObfuscatedStoreCase4Rva, serverID);
    bool channelStored = StoreNativeHudState(
        kHudChannelIndexDescriptorRva, kHudChannelIndexValueRva,
        kHudChannelIndexLockRva, kObfuscatedStoreCase0Rva, channelID);
    LogLine("native channel HUD state committed server=%d channel=%d channel_store=%d server_store=%d",
        serverID, channelID, channelStored ? 1 : 0, serverStored ? 1 : 0);
    return channelStored && serverStored;
}

bool StoreNativeClockValue(
    uintptr_t descriptorRva, uintptr_t valueRva, uintptr_t storeRva, int value)
{
    ObfuscatedStoreFn store = reinterpret_cast<ObfuscatedStoreFn>(
        g_dnfBase + storeRva);
    void* descriptor = reinterpret_cast<void*>(g_dnfBase + descriptorRva);
    int* encryptedValue = reinterpret_cast<int*>(g_dnfBase + valueRva);
    store(descriptor, &value, encryptedValue);
    return true;
}

bool CommitNativeServerClockState()
{
    __time32_t currentTime = _time32(nullptr);
    if (currentTime <= 0) {
        LogLine("native server clock state skipped invalid unix time=%d",
            static_cast<int>(currentTime));
        return false;
    }

    DWORD tick = GetTickCount();
    typedef DWORD (WINAPI* TimeGetTimeFn)();
    HMODULE winmm = GetModuleHandleW(L"winmm.dll");
    TimeGetTimeFn timeGetTimeFn = winmm
        ? reinterpret_cast<TimeGetTimeFn>(
            GetProcAddress(winmm, "timeGetTime"))
        : nullptr;
    if (timeGetTimeFn) {
        tick = timeGetTimeFn();
    }

    bool serverStored = false;
    bool localStored = false;
    bool tickStored = false;
    __try {
        // Current NoPack sub_1D70130 initializes these three obfuscated
        // values from CHANNELINFO field 11, _time32, and timeGetTime. The
        // compatibility connection path commits the same native state after
        // a verified successful channel connection.
        serverStored = StoreNativeClockValue(
            kServerClockDescriptorRva, kServerClockValueRva,
            kObfuscatedStoreCase0Rva, static_cast<int>(currentTime));
        localStored = StoreNativeClockValue(
            kLocalClockDescriptorRva, kLocalClockValueRva,
            kObfuscatedStoreCase8Rva, static_cast<int>(currentTime));
        tickStored = StoreNativeClockValue(
            kServerTickDescriptorRva, kServerTickValueRva,
            kObfuscatedStoreCase0Rva, static_cast<int>(tick));
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("native server clock state failed with structured exception");
        return false;
    }

    LogLine("native server clock state committed unix=%d tick=%u server_store=%d local_store=%d tick_store=%d",
        static_cast<int>(currentTime), static_cast<unsigned int>(tick),
        serverStored ? 1 : 0, localStored ? 1 : 0, tickStored ? 1 : 0);
    return serverStored && localStored && tickStored;
}

bool CommitResidentChannelSnapshot(unsigned short connectedPort)
{
    if (!g_gameWideAssign) {
        LogLine("channel resident snapshot skipped: wide-string assign is unavailable");
        return false;
    }

    __try {
        void* selected = *reinterpret_cast<void**>(g_dnfBase + kSelectedChannelPointerRva);
        if (!selected) {
            LogLine("channel resident snapshot skipped: selected channel is null");
            return false;
        }

        uintptr_t* vtable = *reinterpret_cast<uintptr_t**>(selected);
        ChannelObjectPredicateFn predicateA =
            reinterpret_cast<ChannelObjectPredicateFn>(vtable[20 / sizeof(uintptr_t)]);
        ChannelObjectPredicateFn predicateB =
            reinterpret_cast<ChannelObjectPredicateFn>(vtable[24 / sizeof(uintptr_t)]);
        bool predicateAResult = predicateA(selected);
        bool predicateBResult = predicateB(selected);

        ChannelObjectNameFn getName =
            reinterpret_cast<ChannelObjectNameFn>(vtable[68 / sizeof(uintptr_t)]);
        ChannelObjectIntFn getServerID =
            reinterpret_cast<ChannelObjectIntFn>(vtable[76 / sizeof(uintptr_t)]);
        ChannelObjectIntFn getAddress =
            reinterpret_cast<ChannelObjectIntFn>(vtable[128 / sizeof(uintptr_t)]);
        ChannelObjectPortFn getPort =
            reinterpret_cast<ChannelObjectPortFn>(vtable[132 / sizeof(uintptr_t)]);

        const wchar_t* name = getName(selected);
        if (!name) {
            LogLine("channel resident snapshot skipped: selected=%p name is null", selected);
            return false;
        }
        size_t nameLength = wcsnlen(name, 256);
        if (nameLength == 256) {
            LogLine("channel resident snapshot skipped: selected=%p name is unterminated", selected);
            return false;
        }

        int serverID = getServerID(selected);
        int address = getAddress(selected);
        unsigned short port = getPort(selected);
        int channelID = *reinterpret_cast<int*>(
            static_cast<unsigned char*>(selected) + 0x70);
        unsigned int expectedPort = channelID > 0
            ? static_cast<unsigned int>(kGamePortBase) + static_cast<unsigned int>(channelID)
            : 0;
        if (channelID <= 0 || expectedPort > 0xFFFF ||
            port != static_cast<unsigned short>(expectedPort) ||
            (connectedPort != 0 && port != connectedPort)) {
            LogLine("channel resident snapshot skipped: selected=%p channel=%d port=%u expected_port=%u connected_port=%u predicate_a=%d predicate_b=%d",
                selected, channelID, static_cast<unsigned int>(port), expectedPort,
                static_cast<unsigned int>(connectedPort),
                predicateAResult ? 1 : 0, predicateBResult ? 1 : 0);
            return false;
        }
        void* resident = reinterpret_cast<void*>(g_dnfBase + kResidentChannelRva);
        g_gameWideAssign(resident, name, nameLength);
        *reinterpret_cast<int*>(static_cast<unsigned char*>(resident) + 32) = address;
        *reinterpret_cast<int*>(static_cast<unsigned char*>(resident) + 36) =
            static_cast<int>(port);

        if (!CommitNativeHudChannelState(serverID, channelID)) {
            LogLine("channel resident snapshot skipped HUD commit server=%d channel=%d",
                serverID, channelID);
        }

        LogLine("channel resident snapshot committed selected=%p server=%d channel=%d name_length=%u address=0x%08X port=%u predicate_a=%d predicate_b=%d",
            selected, serverID, channelID, static_cast<unsigned int>(nameLength),
            static_cast<unsigned int>(address),
            static_cast<unsigned int>(port), predicateAResult ? 1 : 0, predicateBResult ? 1 : 0);
        return true;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("channel resident snapshot failed with structured exception");
        return false;
    }
}

bool __fastcall ProxyChannelConnect(void* self, void* /*unused*/, int address, unsigned short port)
{
    unsigned short targetPort = port;
    unsigned long channelID = 11;
    const char* configurationSource = "default";
    wchar_t configured[16] = { 0 };
    DWORD length = GetEnvironmentVariableW(L"DNF_RESIDENT_CHANNEL_ID", configured, 16);
    if (length > 0 && length < 16) {
        wchar_t* end = nullptr;
        unsigned long configuredChannelID = wcstoul(configured, &end, 10);
        if (end && *end == L'\0') {
            channelID = configuredChannelID;
            configurationSource = channelID > 0 ? "environment" : "disabled";
        }
        else {
            LogLine("channel bootstrap invalid configuration; using default channel=%lu", channelID);
        }
    }

    if (port == kCrackBootstrapPort && channelID > 0) {
        unsigned long candidatePort = static_cast<unsigned long>(kGamePortBase) + channelID;
        if (candidatePort <= 0xFFFF) {
            targetPort = static_cast<unsigned short>(candidatePort);
            LogLine("channel bootstrap redirect source=%s source_port=%u target_channel=%lu target_port=%u",
                configurationSource, static_cast<unsigned int>(port), channelID,
                static_cast<unsigned int>(targetPort));
        }
    }

    bool result = g_originalChannelConnect
        ? g_originalChannelConnect(self, address, targetPort)
        : false;
    LogLine("channel connect address=0x%08X requested_port=%u effective_port=%u result=%d",
        static_cast<unsigned int>(address), static_cast<unsigned int>(port),
        static_cast<unsigned int>(targetPort), result ? 1 : 0);
    return result;
}

bool __cdecl ProxyChannelDirectoryApply()
{
    LogLine("channel directory apply enter");
    bool result = g_originalChannelDirectoryApply
        ? g_originalChannelDirectoryApply()
        : false;
    LogLine("channel directory apply return result=%d", result ? 1 : 0);
    return result;
}

bool IsPvpChannelQueryCall(uintptr_t callerRva, unsigned int requestedType)
{
    return (callerRva == kPvpQueryType8ReturnRva && requestedType == 8) ||
        ((callerRva == kPvpQueryType31ReturnRvaA ||
             callerRva == kPvpQueryType31ReturnRvaB) &&
            requestedType == 31) ||
        ((callerRva == kPvpQueryType41ReturnRvaA ||
             callerRva == kPvpQueryType41ReturnRvaB) &&
            requestedType == 41);
}

unsigned int* __stdcall ProxyChannelQuery(unsigned int* output, int type)
{
    uintptr_t callerRva =
        reinterpret_cast<uintptr_t>(_ReturnAddress()) - g_dnfBase;
    unsigned int requestedType = static_cast<unsigned int>(type);
    bool remapped = IsPvpChannelQueryCall(callerRva, requestedType);
    int queryType = remapped ? 24 : type;
    unsigned int* result = g_originalChannelQuery
        ? g_originalChannelQuery(output, queryType)
        : output;
    if (!remapped) {
        return result;
    }

    unsigned int count = 0;
    __try {
        if (output && output[2] >= output[1]) {
            count = (output[2] - output[1]) / sizeof(void*);
        }
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        count = 0xFFFFFFFFu;
    }
    LogLine("pvp channel query remapped caller_rva=0x%08X requested_type=%u effective_type=24 count=%u output=%p",
        callerRva, requestedType, count, output);
    return result;
}

bool InstallChannelUiCompatibility()
{
    static const unsigned char kSehPrologue[] = {
        0x55, 0x8B, 0xEC, 0x6A, 0xFF
    };
    uintptr_t queryTarget = g_dnfBase + kPvpChannelQueryRva;

    if (!BytesMatch(reinterpret_cast<unsigned char*>(queryTarget),
            kSehPrologue, sizeof(kSehPrologue))) {
        LogLine("channel UI compatibility target verification failed query=0x%08X",
            queryTarget);
        return false;
    }

    void* original = nullptr;
    if (!InstallKnownInlineHook(queryTarget, kSehPrologue,
            sizeof(kSehPrologue), reinterpret_cast<void*>(&ProxyChannelQuery),
            &original, "pvp channel query remap")) {
        return false;
    }
    g_originalChannelQuery = reinterpret_cast<ChannelQueryFn>(original);

    LogLine("channel UI compatibility installed query=0x%08X pvp_call_sites=5 effective_type=24 query_type_width=32",
        queryTarget);
    return true;
}

bool InstallChannelDiagnostic()
{
    static const unsigned char kSehPrologue[] = { 0x55, 0x8B, 0xEC, 0x6A, 0xFF };
    uintptr_t downloadTarget = g_dnfBase + kChannelScriptDownloadRva;
    uintptr_t loadTarget = g_dnfBase + kChannelScriptLoadRva;
    uintptr_t directoryApplyTarget = g_dnfBase + kChannelDirectoryApplyRva;
    uintptr_t runtimeLoadTarget = g_dnfBase + kChannelRuntimeLoadRva;
    uintptr_t categoryInsertTarget = g_dnfBase + kChannelCategoryInsertRva;
    uintptr_t connectTarget = g_dnfBase + kChannelConnectRva;
    if (!BytesMatch(reinterpret_cast<unsigned char*>(downloadTarget), kSehPrologue, sizeof(kSehPrologue)) ||
        !BytesMatch(reinterpret_cast<unsigned char*>(loadTarget), kSehPrologue, sizeof(kSehPrologue)) ||
        !BytesMatch(reinterpret_cast<unsigned char*>(directoryApplyTarget), kSehPrologue, sizeof(kSehPrologue)) ||
        !BytesMatch(reinterpret_cast<unsigned char*>(runtimeLoadTarget), kSehPrologue, sizeof(kSehPrologue)) ||
        !BytesMatch(reinterpret_cast<unsigned char*>(categoryInsertTarget), kSehPrologue, sizeof(kSehPrologue)) ||
        !BytesMatch(reinterpret_cast<unsigned char*>(connectTarget), kSehPrologue, sizeof(kSehPrologue))) {
        LogLine("channel diagnostic target verification failed download=0x%08X load=0x%08X apply=0x%08X runtime=0x%08X category=0x%08X connect=0x%08X",
            downloadTarget, loadTarget, directoryApplyTarget, runtimeLoadTarget,
            categoryInsertTarget, connectTarget);
        return false;
    }

    void* original = nullptr;
    if (!InstallKnownInlineHook(connectTarget, kSehPrologue, sizeof(kSehPrologue),
            reinterpret_cast<void*>(&ProxyChannelConnect), &original,
            "channel connect")) return false;
    g_originalChannelConnect = reinterpret_cast<ChannelConnectFn>(original);

    original = nullptr;
    if (!InstallKnownInlineHook(runtimeLoadTarget, kSehPrologue, sizeof(kSehPrologue),
            reinterpret_cast<void*>(&ProxyChannelRuntimeLoad), &original,
            "channel runtime load")) return false;
    g_originalChannelRuntimeLoad = reinterpret_cast<ChannelRuntimeLoadFn>(original);

    original = nullptr;
    if (!InstallKnownInlineHook(categoryInsertTarget, kSehPrologue, sizeof(kSehPrologue),
            reinterpret_cast<void*>(&ProxyChannelCategoryInsert), &original,
            "channel category insert")) return false;
    g_originalChannelCategoryInsert = reinterpret_cast<ChannelCategoryInsertFn>(original);

    original = nullptr;
    if (!InstallKnownInlineHook(directoryApplyTarget, kSehPrologue, sizeof(kSehPrologue),
            reinterpret_cast<void*>(&ProxyChannelDirectoryApply), &original,
            "channel directory apply")) return false;
    g_originalChannelDirectoryApply = reinterpret_cast<ChannelDirectoryApplyFn>(original);

    original = nullptr;
    if (!InstallKnownInlineHook(loadTarget, kSehPrologue, sizeof(kSehPrologue),
            reinterpret_cast<void*>(&ProxyChannelScriptLoad), &original,
            "channel script load")) return false;
    g_originalChannelScriptLoad = reinterpret_cast<ChannelScriptLoadFn>(original);

    original = nullptr;
    if (!InstallKnownInlineHook(downloadTarget, kSehPrologue, sizeof(kSehPrologue),
            reinterpret_cast<void*>(&ProxyChannelScriptDownload), &original,
            "channel script download")) return false;
    g_originalChannelScriptDownload = reinterpret_cast<ChannelScriptDownloadFn>(original);
    return true;
}

bool InstallTclsCompatibility()
{
    static const unsigned char kSehPrologue[] = { 0x55, 0x8B, 0xEC, 0x6A, 0xFF };
    static const unsigned char kFetchTextPrologue[] = { 0x55, 0x8B, 0xEC, 0x81, 0xEC, 0x08, 0x01, 0x00, 0x00 };
    static const unsigned char kFetchTailPrologue[] = { 0x55, 0x8B, 0xEC, 0x81, 0xEC, 0x8C, 0x04, 0x00, 0x00 };

    uintptr_t parseTarget = g_dnfBase + kTclsParseRva;
    uintptr_t loginTarget = g_dnfBase + kTclsFetchLoginRva;
    uintptr_t textTarget = g_dnfBase + kTclsFetchTextRva;
    uintptr_t cryptoTarget = g_dnfBase + kTclsFetchCryptoRva;
    uintptr_t fetchTailTarget = g_dnfBase + kTclsFetchTailRva;
    uintptr_t tailTarget = g_dnfBase + kTclsTailRva;
    g_gameWideAssign = reinterpret_cast<GameWideAssignFn>(g_dnfBase + kGameWideAssignRva);

    // Validate the complete set before changing any entry point.  These are
    // current-EXE local launch helpers, never send/recv or packet parsers.
    if (!BytesMatch(reinterpret_cast<unsigned char*>(parseTarget), kSehPrologue, sizeof(kSehPrologue)) ||
        !BytesMatch(reinterpret_cast<unsigned char*>(loginTarget), kSehPrologue, sizeof(kSehPrologue)) ||
        !BytesMatch(reinterpret_cast<unsigned char*>(textTarget), kFetchTextPrologue, sizeof(kFetchTextPrologue)) ||
        !BytesMatch(reinterpret_cast<unsigned char*>(cryptoTarget), kSehPrologue, sizeof(kSehPrologue)) ||
        !BytesMatch(reinterpret_cast<unsigned char*>(fetchTailTarget), kFetchTailPrologue, sizeof(kFetchTailPrologue)) ||
        !BytesMatch(reinterpret_cast<unsigned char*>(tailTarget), kSehPrologue, sizeof(kSehPrologue))) {
        LogLine("TCLS current-EXE target verification failed; no TCLS hook installed");
        return false;
    }

    void* original = nullptr;
    if (!InstallKnownInlineHook(loginTarget, kSehPrologue, sizeof(kSehPrologue),
            reinterpret_cast<void*>(&ProxyTclsFetchLogin), &original, "tcls fetch login")) return false;
    g_originalTclsFetchLogin = reinterpret_cast<TclsFetchLoginFn>(original);

    original = nullptr;
    if (!InstallKnownInlineHook(textTarget, kFetchTextPrologue, sizeof(kFetchTextPrologue),
            reinterpret_cast<void*>(&ProxyTclsFetchText), &original, "tcls fetch text")) return false;
    g_originalTclsFetchText = reinterpret_cast<TclsFetchTextFn>(original);

    original = nullptr;
    if (!InstallKnownInlineHook(cryptoTarget, kSehPrologue, sizeof(kSehPrologue),
            reinterpret_cast<void*>(&ProxyTclsFetchCrypto), &original, "tcls fetch crypto")) return false;
    g_originalTclsFetchCrypto = reinterpret_cast<TclsFetchOneFn>(original);

    original = nullptr;
    if (!InstallKnownInlineHook(fetchTailTarget, kFetchTailPrologue, sizeof(kFetchTailPrologue),
            reinterpret_cast<void*>(&ProxyTclsFetchTail), &original, "tcls fetch tail")) return false;
    g_originalTclsFetchTail = reinterpret_cast<TclsFetchOneFn>(original);

    original = nullptr;
    if (!InstallKnownInlineHook(tailTarget, kSehPrologue, sizeof(kSehPrologue),
            reinterpret_cast<void*>(&ProxyTclsTail), &original, "tcls tail")) return false;
    g_originalTclsTail = reinterpret_cast<TclsTailFn>(original);

    // Parse is installed last because all fetches occur recursively inside
    // the original parse call and must already have valid trampolines.
    original = nullptr;
    if (!InstallKnownInlineHook(parseTarget, kSehPrologue, sizeof(kSehPrologue),
            reinterpret_cast<void*>(&ProxyTclsParse), &original, "tcls parse")) return false;
    g_originalTclsParse = reinterpret_cast<TclsParseFn>(original);

    LogLine("TCLS local launch compatibility installed (six native helpers)");
    return true;
}

bool InstallCodecSetKeyTrace()
{
    static const unsigned char kSetKey0Prologue[] = { 0x55, 0x8B, 0xEC, 0x8B, 0x49, 0x08 };
    static const unsigned char kSetKey2Prologue[] = { 0x55, 0x8B, 0xEC, 0x8B, 0x51, 0x08 };
    static const unsigned char kSetKey3Prologue[] = { 0x55, 0x8B, 0xEC, 0x56, 0x8B, 0xF1 };
    static const unsigned char kSetKey7Prologue[] = { 0x55, 0x8B, 0xEC, 0x6A, 0xFF };
    static const unsigned char kSetKey8Prologue[] = { 0x55, 0x8B, 0xEC, 0x56, 0x8B, 0xF1 };

    uintptr_t target0 = g_dnfBase + kCodecSetKey0Rva;
    uintptr_t target2 = g_dnfBase + kCodecSetKey2Rva;
    uintptr_t target3 = g_dnfBase + kCodecSetKey3Rva;
    uintptr_t target7 = g_dnfBase + kCodecSetKey7Rva;
    uintptr_t target8 = g_dnfBase + kCodecSetKey8Rva;

    constexpr int kWaitLimitMs = 120000;
    int waitedMs = 0;
    while (waitedMs < kWaitLimitMs) {
        if (BytesMatch(reinterpret_cast<unsigned char*>(target0), kSetKey0Prologue, sizeof(kSetKey0Prologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(target2), kSetKey2Prologue, sizeof(kSetKey2Prologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(target3), kSetKey3Prologue, sizeof(kSetKey3Prologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(target7), kSetKey7Prologue, sizeof(kSetKey7Prologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(target8), kSetKey8Prologue, sizeof(kSetKey8Prologue))) {
            break;
        }
        Sleep(50);
        waitedMs += 50;
    }
    if (waitedMs >= kWaitLimitMs) {
        LogLine("codec setkey trace targets did not reach verified bytes; no setkey hooks installed");
        return false;
    }

    void* original = nullptr;
    if (!InstallKnownInlineHook(target0, kSetKey0Prologue, sizeof(kSetKey0Prologue),
            reinterpret_cast<void*>(&ProxyCodecSetKey0), &original, "codec setkey idx0")) return false;
    g_originalCodecSetKey0 = reinterpret_cast<CodecSetKeyFn>(original);

    original = nullptr;
    if (!InstallKnownInlineHook(target2, kSetKey2Prologue, sizeof(kSetKey2Prologue),
            reinterpret_cast<void*>(&ProxyCodecSetKey2), &original, "codec setkey idx2")) return false;
    g_originalCodecSetKey2 = reinterpret_cast<CodecSetKeyFn>(original);

    original = nullptr;
    if (!InstallKnownInlineHook(target3, kSetKey3Prologue, sizeof(kSetKey3Prologue),
            reinterpret_cast<void*>(&ProxyCodecSetKey3), &original, "codec setkey idx3")) return false;
    g_originalCodecSetKey3 = reinterpret_cast<CodecSetKeyFn>(original);

    original = nullptr;
    if (!InstallKnownInlineHook(target7, kSetKey7Prologue, sizeof(kSetKey7Prologue),
            reinterpret_cast<void*>(&ProxyCodecSetKey7), &original, "codec setkey idx7")) return false;
    g_originalCodecSetKey7 = reinterpret_cast<CodecSetKeyFn>(original);

    original = nullptr;
    if (!InstallKnownInlineHook(target8, kSetKey8Prologue, sizeof(kSetKey8Prologue),
            reinterpret_cast<void*>(&ProxyCodecSetKey8), &original, "codec setkey idx8")) return false;
    g_originalCodecSetKey8 = reinterpret_cast<CodecSetKeyFn>(original);

    LogLine("codec setkey trace installed wait_ms=%d targets=0x%08X,0x%08X,0x%08X,0x%08X,0x%08X",
        waitedMs, target0, target2, target3, target7, target8);
    return true;
}

int __cdecl TryUpperBodyEncodeBypass(int messageType, char* input, int bodyLength, char* output, int* outLength)
{
    LONG callNumber = InterlockedIncrement(&g_cipherCallCount);
    int capacity = 0;
    __try {
        capacity = outLength ? *outLength : 0;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        capacity = 0;
    }

    int emitLength = bodyLength;
    if (messageType == 4) emitLength = 16;
    if (messageType != 4 && messageType != 5) {
        return 0;
    }
    if (!input || !output || !outLength || bodyLength <= 0 || bodyLength > 0x200 || capacity < emitLength) {
        if (callNumber <= 32) {
            LogLine("upper-body-encode-bypass-skip call=%ld msg=%d len=%d cap=%d emit=%d input=%p output=%p out_len=%p",
                callNumber, messageType, bodyLength, capacity, emitLength, input, output, outLength);
        }
        return 0;
    }

    __try {
        if (messageType == 4) {
            memset(output, 0, 16);
            int copyLength = bodyLength < 16 ? bodyLength : 16;
            memcpy(output, input, static_cast<size_t>(copyLength));
        } else {
            memcpy(output, input, static_cast<size_t>(bodyLength));
        }
        *outLength = emitLength;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("upper-body-encode-bypass exception call=%ld msg=%d len=%d code=0x%08X",
            callNumber, messageType, bodyLength, GetExceptionCode());
        return 0;
    }

    if (callNumber <= 64) {
        char hex[360] = { 0 };
        FormatHexBytes(output, emitLength, hex, sizeof(hex));
        LogLine("upper-body-encode-bypass call=%ld msg=%d len=%d emit_len=%d cap=%d body=%s",
            callNumber, messageType, bodyLength, emitLength, capacity, hex);
    }
    return 1;
}

unsigned int __cdecl ReadSelectedQuestFromAutoCompleteCallback(void* callback)
{
    unsigned int selector = 0;
    __try {
        if (callback) {
            selector = *reinterpret_cast<unsigned int*>(
                static_cast<unsigned char*>(callback) + 0x30);
        }
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("quest-auto-complete selected read exception callback=%p code=0x%08X",
            callback, GetExceptionCode());
        return 0;
    }

    LONG callNumber = InterlockedIncrement(&g_questAutoCompleteCallCount);
    LogLine("quest-auto-complete selected call=%ld callback=%p selector=%u mode=%s",
        callNumber, callback, selector, selector ? "single" : "native-zero-fallback");
    return selector;
}

// The current task window captures the selected row's exact quest id at
// callback+0x30 in sub_304E070, but its confirmation callback discards that
// value and calls sub_19EE300(mode=1, selector=-1), which serializes u32(0).
// Rewrite only this verified call-site to the native single-row mode.  When a
// selector cannot be read, preserve the original zero-selector call so the
// server's ambiguity guard fails closed without mutating any quest.
__declspec(naked) void ProxyQuestAutoCompleteSelected()
{
    __asm {
        push ecx
        push edx
        push edi
        call ReadSelectedQuestFromAutoCompleteCallback
        add esp, 4
        pop edx
        pop ecx

        test eax, eax
        jz quest_auto_complete_fallback
        mov dword ptr [esp + 4], 0
        mov dword ptr [esp + 8], eax

    quest_auto_complete_fallback:
        jmp dword ptr [g_questAutoCompleteSendFn]
    }
}

// Function-head proxy for sub_33AAFE0.  This matches the current-EXE universal
// upper-body codec boundary: the caller has already prepared the packet header
// immediately before `input`.  Copy the semantic plaintext body to the native
// output buffer, repair the total length and checksum, and return success using
// the native 5-stack-argument cleanup contract.  This covers the whole
// opcode%14 codec family without per-opcode filters or socket hooks.
__declspec(naked) void ProxyCipherEncodeFunction()
{
    __asm {
        // Preserve the native register/flags contract while recording the
        // plaintext semantic body before it is copied into the send buffer.
        pushfd
        pushad
        mov eax, [esp + 28h]      // messageType
        mov ecx, [esp + 2Ch]      // input
        mov edx, [esp + 30h]      // bodyLength
        push edx
        push ecx
        push eax
        call LogClientToServerPacket
        add esp, 0Ch
        popad
        popfd

        push esi
        push edi
        push ebx

        // Entry stack before preserving registers:
        // [esp+04]=msg, [esp+08]=input, [esp+0C]=bodyLength,
        // [esp+10]=output, [esp+14]=outLength*
        mov ebx, [esp + 14h]      // input after three pushes
        mov ecx, [esp + 18h]      // bodyLength
        test ecx, ecx
        jle cipher_zero_case

        mov esi, ebx
        mov edi, [esp + 1Ch]      // output
        rep movsb                 // memcpy(output, input, bodyLength)

        mov eax, [esp + 20h]      // outLength*
        mov ecx, [esp + 18h]
        mov [eax], ecx

        lea edx, [ecx + 0Dh]
        mov [ebx - 0Ah], edx      // packet total length = bodyLength + 13

        lea eax, [ecx + 02h]
        push eax                  // crc length = bodyLength + sequence(2)
        lea eax, [ebx - 02h]
        push eax                  // crc begins at sequence
        call dword ptr [g_checksumFn]
        add esp, 08h
        mov [ebx - 06h], eax

        mov eax, 1
        pop ebx
        pop edi
        pop esi
        ret 14h

    cipher_zero_case:
        mov eax, [esp + 20h]
        mov dword ptr [eax], 0
        mov eax, 1
        pop ebx
        pop edi
        pop esi
        ret 14h
    }
}

// Function-head proxy for sub_33AB0A0, the current-EXE S2C upper-body decode
// boundary.  The server-side chain is intentionally plaintext while this DLL
// is active, so copy the received semantic body into the native output buffer
// and report success using the same 5-stack-argument cleanup contract.  No
// packet headers, socket bytes, opcodes, or routing decisions are touched here.
__declspec(naked) void ProxyCipherDecodeFunction()
{
    __asm {
        // Log the exact plaintext body handed to the native S2C dispatcher.
        pushfd
        pushad
        mov eax, [esp + 28h]      // messageType
        mov ecx, [esp + 2Ch]      // input
        mov edx, [esp + 30h]      // bodyLength
        push edx
        push ecx
        push eax
        call LogServerToClientPacket
        add esp, 0Ch
        popad
        popfd

        push esi
        push edi
        push ebx

        // Entry stack before preserving registers:
        // [esp+04]=msg, [esp+08]=input, [esp+0C]=bodyLength,
        // [esp+10]=output, [esp+14]=outLength*
        mov ebx, [esp + 14h]      // input after three pushes
        mov ecx, [esp + 18h]      // bodyLength
        test ecx, ecx
        jle cipher_decode_zero_case

        mov esi, ebx
        mov edi, [esp + 1Ch]      // output
        rep movsb                 // memcpy(output, input, bodyLength)

        mov eax, [esp + 20h]      // outLength*
        mov ecx, [esp + 18h]
        mov [eax], ecx

        mov eax, 1
        pop ebx
        pop edi
        pop esi
        ret 14h

    cipher_decode_zero_case:
        mov eax, [esp + 20h]
        mov dword ptr [eax], 0
        mov eax, 1
        pop ebx
        pop edi
        pop esi
        ret 14h
    }
}

// DPROTO compatibility mode, outbound direction.
//
// This is entered after sub_34738E0 has finalized the ordinary inner packet.
// The original function would call the mode-7 client protector and wrap the
// result as op1517.  We instead clone that finalized packet into the existing
// output ownership contract, for every opcode.  There is deliberately no
// opcode comparison, allow list, network hook, or captured body involved.
__declspec(naked) void ProxyDprotoSendDirect()
{
    __asm {
        // Load the opcode only for log/probe parity with the historical PS1
        // trampoline. Do not let it affect the cloned packet pointer.
        movzx eax, word ptr [esi + 1]

        // The native CRC call immediately before this entry is cdecl; discard
        // its two arguments exactly as the original path does at 0x3473924.
        add esp, 8

        // EDI = finalized inner packet length; ESI = source inner packet.
        push edi
        call dword ptr [g_gameAllocatorFn]
        add esp, 4
        test eax, eax
        jz dproto_oom

        // memcpy(destination=eax, source=esi, length=edi)
        push edi
        push esi
        push eax
        call dword ptr [g_gameMemcpyFn]
        add esp, 0Ch

        // Restore the sequence expected by upper_pkt_flush_send13 and
        // recompute the copied packet's checksum over sequence + body.
        mov dx, word ptr [ebp + 10h]
        mov word ptr [eax + 0Bh], dx
        push eax
        lea edx, [eax + 0Bh]
        mov ecx, edi
        sub ecx, 0Bh
        push ecx
        push edx
        call dword ptr [g_checksumFn]
        add esp, 8
        pop edx
        mov dword ptr [edx + 7], eax

        mov ecx, dword ptr [ebp + 14h]
        mov dword ptr [ecx], edx
        jmp dword ptr [g_dprotoOutgoingReturn]

    dproto_oom:
        mov edx, dword ptr [ebp + 14h]
        mov dword ptr [edx], 0
        jmp dword ptr [g_dprotoOutgoingReturn]
    }
}

// Predicate-false DPROTO compatibility. The native branch at 0x34738FA skips
// the TerSafe wrapper for some requests; only commands proven by the PS1 route
// are cloned here. Every other opcode resumes the native false epilogue.
__declspec(naked) void ProxyDprotoFalseSelective()
{
    __asm {
        movzx eax, word ptr [esi + 1]

        cmp ax, 64
        je dproto_false_clone
        cmp ax, 16
        je dproto_false_clone
        cmp ax, 19
        je dproto_false_clone
        cmp ax, 28
        je dproto_false_clone
        cmp ax, 29
        je dproto_false_clone
        cmp ax, 31
        je dproto_false_clone
        cmp ax, 33
        je dproto_false_clone
        cmp ax, 34
        je dproto_false_clone
        cmp ax, 35
        je dproto_false_clone
        cmp ax, 36
        je dproto_false_clone
        cmp ax, 39
        je dproto_false_clone
        cmp ax, 40
        je dproto_false_clone
        cmp ax, 41
        je dproto_false_clone
        cmp ax, 42
        je dproto_false_clone
        cmp ax, 43
        je dproto_false_clone
        cmp ax, 44
        je dproto_false_clone
        cmp ax, 45
        je dproto_false_clone
        cmp ax, 46
        je dproto_false_clone
        cmp ax, 71
        je dproto_false_clone
        cmp ax, 72
        je dproto_false_clone
        cmp ax, 117
        je dproto_false_clone
        cmp ax, 123
        je dproto_false_clone
        cmp ax, 132
        je dproto_false_clone
        cmp ax, 143
        je dproto_false_clone
        cmp ax, 491
        je dproto_false_clone
        cmp ax, 1437
        je dproto_false_clone
        cmp ax, 1443
        je dproto_false_clone
        cmp ax, 999
        je dproto_false_clone
        cmp ax, 1000
        je dproto_false_clone

        jmp dword ptr [g_dprotoFalseResume]

    dproto_false_clone:
        push edi
        mov edi, dword ptr [ebp + 0Ch]

        push edi
        call dword ptr [g_gameAllocatorFn]
        add esp, 4
        test eax, eax
        jz dproto_false_oom

        // memcpy(destination=eax, source=[ebp+08h], length=edi)
        push edi
        mov edx, dword ptr [ebp + 08h]
        push edx
        push eax
        call dword ptr [g_gameMemcpyFn]
        add esp, 0Ch

        // False path packets store total length at +3, then sequence/CRC.
        mov dword ptr [eax + 03h], edi
        mov dx, word ptr [ebp + 10h]
        mov word ptr [eax + 0Bh], dx
        mov esi, eax

        lea edx, [edi - 0Bh]
        push edx
        lea edx, [esi + 0Bh]
        push edx
        call dword ptr [g_checksumFn]
        add esp, 8
        mov dword ptr [esi + 07h], eax

        mov ecx, dword ptr [ebp + 14h]
        mov dword ptr [ecx], esi
        pop edi
        jmp dword ptr [g_dprotoFalseResume]

    dproto_false_oom:
        pop edi
        jmp dword ptr [g_dprotoFalseResume]
    }
}

bool __stdcall IsSceneRouteOpcodeAllowed(uint16_t opcode)
{
    switch (opcode) {
    // Login / character / scene bootstrap packets that are built by the Go
    // server with current-EXE bodies.
    case 2:
    case 3:
    case 4:
    case 5:
    case 6:
    case 7:
    case 8:
    case 9:
    // Current EXE class1 handlers sub_1D11A80/sub_1D12480 own the town peer
    // request/response handshake used by the remote-player context menu.
    case 10:
    case 11:
    case 12:
    case 13:
    case 14:
    case 15:
    case 16:
    case 18:
    // Current EXE class0/op19 installs the complete learned-skill trees and
    // quick slots. The same opcode's class1 handler owns MOVE_ITEMSPACE ACKs
    // and performs the native equipment/avatar/creature slot relocation.
    case 19:
    case 20:
    case 21:
    case 22:
    case 23:
    case 24:
    case 26:
    case 27:
    case 28:
    case 29:
    case 30:
    case 31:
    case 32:
    case 33:
    case 34:
    case 35:
    case 36:
    case 37:
    case 38:
    case 39:
    case 40:
    case 41:
    case 42:
    case 43:
    case 44:
    case 45:
    case 46:
    case 48:
    case 53:
    case 63:
    case 64:
    case 66:
    case 80:
    case 81: // CmdPacketResetItemAttr - equipment grade-adjust result popup
    case 94:
    case 95:
    case 96:
    case 99:
    case 100:
    case 101:
    case 102:
    // Current NoPack sub_1D57AB0 consumes scene class0/op105 as the
    // creature-status table. Without route admission, slot26 relocation can
    // succeed through op19 while the creature UI remains null/uninitialized.
    case 105:
    // Current NoPack sub_20457F0 owns the activity descriptor and
    // sub_E713E0 owns event progress. Event 2347 lazily creates the cumulative
    // online-time UI from this ordered op108 -> op1206 pair.
    case 108:
    case 115:
    case 120:
    case 124:
    case 132:
    case 138:
    case 143:
    case 149:
    case 153:
    case 160:
    case 166:
    case 173:
    case 174:
    case 191:
    case 200:
    case 201:
    case 202:
    case 206:
    case 208:
    case 237:
    case 256:
    case 260:
    case 267:
    case 268:
    case 269:
    case 272:
    case 273:
    case 295:
    case 305:
    case 306:
    case 307:
    case 308:
    case 331:
    case 332:
    case 356:
    case 357:
    case 358:
    case 359:
    case 376:
    case 391:
    case 392:
    case 400:
    case 412:
    case 413:
    case 491:
    case 518:
    case 523:
    case 558:
    case 559:
    case 560:
    case 574:
    case 635:
    case 692:
    case 734:
    case 800:
    case 899:
    case 913:
    case 914: // CmdPacketAddEquipmentSocket — equipment socket open ACK
    // Weapon-effect rune success ACK. The native handler applies the selected
    // rune to the target weapon from the acknowledged request body; blocking
    // this opcode at the DPROTO route predicate leaves the server mutation
    // durable but prevents the client from updating its equipped item object.
    case 951: // CmdPacketAddEquipmentEffect
    case 985:
    case 993:
    case 994:
    case 997:
    case 1128: // CmdPacketUseRandomboxItemExpand — random-box ten-open result UI
    case 1206:
    case 1220:
    case 1307:
    case 1340:
    case 1346:
    case 1371:
    case 1378:
    case 1403:
    case 1425:
    case 1429:
    case 1430:
    case 1437:
    case 1443:
        return true;
    default:
        return false;
    }
}

// Current-scene inbound route compatibility. This is deliberately narrower
// than ProxyDprotoAcceptDirect: proven scene/UI opcodes are admitted to the
// direct upper reader, and every other opcode tail-calls the native predicate.
// The opcode argument is at [esp+4], matching the live PS1 probe.
__declspec(naked) void ProxySceneRouteAllowList()
{
    __asm {
        movzx eax, word ptr [esp + 4]
        push ecx
        push eax
        call IsSceneRouteOpcodeAllowed
        pop ecx
        test al, al
        jnz scene_route_allow

        // Native fallback: routeObject = [ecx+0x0c], then tail-call vtable+0x40.
        mov ecx, dword ptr [ecx + 0Ch]
        mov eax, dword ptr [ecx]
        mov eax, dword ptr [eax + 40h]
        jmp eax

    scene_route_allow:
        mov eax, 1
        ret 4
    }
}

bool BytesMatch(const unsigned char* address, const unsigned char* expected, size_t count)
{
    __try {
        for (size_t i = 0; i < count; ++i) {
            if (address[i] != expected[i]) return false;
        }
        return true;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        return false;
    }
}

bool TryReadByte(const unsigned char* address, unsigned char* value)
{
    __try {
        *value = *address;
        return true;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        *value = 0;
        return false;
    }
}

bool WriteCodePatch(uintptr_t target, const unsigned char* patch, size_t count, const char* label)
{
    DWORD oldProtection = 0;
    if (!VirtualProtect(reinterpret_cast<void*>(target), count, PAGE_EXECUTE_READWRITE, &oldProtection)) {
        LogLine("%s VirtualProtect failed: %u", label, GetLastError());
        return false;
    }

    memcpy(reinterpret_cast<void*>(target), patch, count);
    DWORD ignoredProtection = 0;
    VirtualProtect(reinterpret_cast<void*>(target), count, oldProtection, &ignoredProtection);
    FlushInstructionCache(GetCurrentProcess(), reinterpret_cast<void*>(target), count);
    return true;
}

bool HookMainModuleImport(const char* targetDll, const char* targetName, void* detour, void** original)
{
    if (!targetDll || !targetName || !detour || !original) return false;
    HMODULE module = GetModuleHandleW(nullptr);
    if (!module) return false;

    __try {
        auto* dos = reinterpret_cast<IMAGE_DOS_HEADER*>(module);
        if (dos->e_magic != IMAGE_DOS_SIGNATURE) return false;
        auto* nt = reinterpret_cast<IMAGE_NT_HEADERS*>(
            reinterpret_cast<unsigned char*>(module) + dos->e_lfanew);
        if (nt->Signature != IMAGE_NT_SIGNATURE) return false;

        IMAGE_DATA_DIRECTORY importDirectory =
            nt->OptionalHeader.DataDirectory[IMAGE_DIRECTORY_ENTRY_IMPORT];
        if (!importDirectory.VirtualAddress || !importDirectory.Size) {
            LogLine("iat hook skipped name=%s reason=no-import-directory", targetName);
            return false;
        }

        auto* descriptor = reinterpret_cast<IMAGE_IMPORT_DESCRIPTOR*>(
            reinterpret_cast<unsigned char*>(module) + importDirectory.VirtualAddress);
        for (; descriptor->Name; ++descriptor) {
            const char* dllName = reinterpret_cast<const char*>(
                reinterpret_cast<unsigned char*>(module) + descriptor->Name);
            if (!dllName || _stricmp(dllName, targetDll) != 0) continue;

            auto* nameThunk = reinterpret_cast<IMAGE_THUNK_DATA*>(
                reinterpret_cast<unsigned char*>(module) + descriptor->OriginalFirstThunk);
            auto* addressThunk = reinterpret_cast<IMAGE_THUNK_DATA*>(
                reinterpret_cast<unsigned char*>(module) + descriptor->FirstThunk);
            if (!descriptor->OriginalFirstThunk) nameThunk = addressThunk;

            for (; nameThunk->u1.AddressOfData && addressThunk->u1.Function; ++nameThunk, ++addressThunk) {
                if (IMAGE_SNAP_BY_ORDINAL(nameThunk->u1.Ordinal)) continue;
                auto* importByName = reinterpret_cast<IMAGE_IMPORT_BY_NAME*>(
                    reinterpret_cast<unsigned char*>(module) + nameThunk->u1.AddressOfData);
                const char* importName = reinterpret_cast<const char*>(importByName->Name);
                if (!importName || strcmp(importName, targetName) != 0) continue;

                void* current = reinterpret_cast<void*>(addressThunk->u1.Function);
                if (current == detour) {
                    LogLine("iat hook already installed name=%s slot=%p", targetName, addressThunk);
                    return true;
                }

                DWORD oldProtection = 0;
                if (!VirtualProtect(&addressThunk->u1.Function, sizeof(addressThunk->u1.Function),
                        PAGE_EXECUTE_READWRITE, &oldProtection)) {
                    LogLine("iat hook protect failed name=%s slot=%p gle=%lu",
                        targetName, addressThunk, GetLastError());
                    return false;
                }

                *original = current;
                addressThunk->u1.Function = reinterpret_cast<ULONG_PTR>(detour);
                DWORD ignoredProtection = 0;
                VirtualProtect(&addressThunk->u1.Function, sizeof(addressThunk->u1.Function),
                    oldProtection, &ignoredProtection);
                FlushInstructionCache(GetCurrentProcess(), &addressThunk->u1.Function,
                    sizeof(addressThunk->u1.Function));
                LogLine("iat hook name=%s dll=%s slot=%p old=%p detour=%p",
                    targetName, dllName, addressThunk, current, detour);
                return true;
            }
        }
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("iat hook exception name=%s code=0x%08X", targetName, GetExceptionCode());
        return false;
    }

    LogLine("iat hook skipped name=%s dll=%s reason=not-imported", targetName, targetDll);
    return false;
}

bool InstallUiCompatibilityHook()
{
    void* sendMessageOriginal = nullptr;
    bool sendMessageInstalled = HookMainModuleImport("USER32.dll", "SendMessageW",
        reinterpret_cast<void*>(&ProxySendMessageW), &sendMessageOriginal);
    if (sendMessageInstalled && sendMessageOriginal && !g_originalSendMessageW) {
        g_originalSendMessageW = sendMessageOriginal;
    }

    void* showWindowOriginal = nullptr;
    bool showWindowInstalled = HookMainModuleImport("USER32.dll", "ShowWindow",
        reinterpret_cast<void*>(&ProxyShowWindow), &showWindowOriginal);
    if (showWindowInstalled && showWindowOriginal && !g_originalShowWindow) {
        g_originalShowWindow = showWindowOriginal;
    }

    void* coCreateOriginal = nullptr;
    bool coCreateInstalled = HookMainModuleImport("ole32.dll", "CoCreateInstance",
        reinterpret_cast<void*>(&ProxyCoCreateInstance), &coCreateOriginal);
    if (coCreateInstalled && coCreateOriginal && !g_originalCoCreateInstance) {
        g_originalCoCreateInstance = coCreateOriginal;
    }

    bool installed = sendMessageInstalled && showWindowInstalled && coCreateInstalled;
    LogLine("[nomin] audited current-EXE IAT branches send=%d show=%d toggle_desktop=%d",
        sendMessageInstalled ? 1 : 0,
        showWindowInstalled ? 1 : 0,
        coCreateInstalled ? 1 : 0);
    return installed;
}

bool InstallMultiClientCompatibilityHook()
{
    if (!IsMultiClientLaunch()) {
        LogLine("[multi] compatibility disabled for normal client launch");
        return true;
    }

    void* createMutexOriginal = nullptr;
    bool installed = HookMainModuleImport(
        "KERNEL32.dll",
        "CreateMutexW",
        reinterpret_cast<void*>(&ProxyCreateMutexW),
        &createMutexOriginal);
    if (installed && createMutexOriginal && !g_originalCreateMutexW) {
        g_originalCreateMutexW = createMutexOriginal;
    }
    void* findWindowOriginal = nullptr;
    bool findWindowInstalled = HookMainModuleImport(
        "USER32.dll",
        "FindWindowW",
        reinterpret_cast<void*>(&ProxyFindWindowW),
        &findWindowOriginal);
    if (findWindowInstalled && findWindowOriginal && !g_originalFindWindowW) {
        g_originalFindWindowW = findWindowOriginal;
    }
    LogLine(
        "[multi] audited current-EXE hooks mutex=%d window=%d mutex_return_rva=0x%08X window_return_rva=0x%08X",
        installed ? 1 : 0,
        findWindowInstalled ? 1 : 0,
        kMultiClientCreateMutexReturnRva,
        kMultiClientFindWindowReturnRva);
    return installed && findWindowInstalled;
}

bool InstallSocketOpenTrace()
{
    // Both signatures include whole instructions only.  A mismatch means this
    // executable build is not the audited NoPack image, so leave it untouched.
    static const unsigned char kItemRowParsePrologue[] = { 0x55, 0x8B, 0xEC, 0x6A, 0xFF };
    static const unsigned char kPanelSelectPrologue[] = { 0x55, 0x8B, 0xEC, 0x8B, 0x45, 0x08 };
    // 53 56 8B F1 is followed by a six-byte mov; keep whole instructions in
    // the trampoline patch rather than splitting that instruction at byte 5.
    static const unsigned char kSocketWriterPrologue[] = {
        0x53, 0x56, 0x8B, 0xF1, 0x8B, 0x86, 0xB4, 0x01, 0x00, 0x00
    };
    // push ebp; mov ebp,esp; mov eax,[ebp+8]; mov ecx,[imm32]
    static const unsigned char kSocketOp14ExtensionGatePrologue[] = {
        0x55, 0x8B, 0xEC, 0x8B, 0x45, 0x08, 0x8B, 0x0D, 0xB8, 0x26, 0x1B, 0x05
    };
    uintptr_t itemRowParseTarget = g_dnfBase + kCurrentItemRowParseRva;
    uintptr_t panelSelectTarget = g_dnfBase + kSocketPanelSelectRva;
    uintptr_t socketWriterTarget = g_dnfBase + kSocketOpenWriterRva;
    uintptr_t socketOp14ExtensionGateTarget = g_dnfBase + kSocketOp14ExtensionGateRva;

    constexpr int kWaitLimitMs = 120000;
    int waitedMs = 0;
    while (waitedMs < kWaitLimitMs) {
        if (BytesMatch(reinterpret_cast<unsigned char*>(itemRowParseTarget),
                kItemRowParsePrologue, sizeof(kItemRowParsePrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(panelSelectTarget),
                kPanelSelectPrologue, sizeof(kPanelSelectPrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(socketWriterTarget),
                kSocketWriterPrologue, sizeof(kSocketWriterPrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(socketOp14ExtensionGateTarget),
                kSocketOp14ExtensionGatePrologue, sizeof(kSocketOp14ExtensionGatePrologue))) {
            break;
        }
        Sleep(250);
        waitedMs += 250;
    }
    if (waitedMs >= kWaitLimitMs) {
        LogLine("socket-trace targets did not reach audited bytes; no trace hook installed row=0x%08X panel=0x%08X writer=0x%08X gate=0x%08X",
            itemRowParseTarget, panelSelectTarget, socketWriterTarget, socketOp14ExtensionGateTarget);
        return false;
    }

    void* original = nullptr;
    if (!InstallKnownInlineHook(itemRowParseTarget, kItemRowParsePrologue,
            sizeof(kItemRowParsePrologue), reinterpret_cast<void*>(&ProxyCurrentItemRowParse),
            &original, "socket-trace item-row parser")) {
        return false;
    }
    g_originalCurrentItemRowParse = reinterpret_cast<CurrentItemRowParseFn>(original);

    original = nullptr;
    if (!InstallKnownInlineHook(panelSelectTarget, kPanelSelectPrologue,
            sizeof(kPanelSelectPrologue), reinterpret_cast<void*>(&ProxySocketPanelSelect),
            &original, "socket-trace panel selection")) {
        LogLine("socket-trace panel hook failed; parser remains passive and silent");
        return false;
    }
    g_originalSocketPanelSelect = reinterpret_cast<SocketPanelSelectFn>(original);

    original = nullptr;
    if (!InstallKnownInlineHook(socketWriterTarget, kSocketWriterPrologue,
            sizeof(kSocketWriterPrologue), reinterpret_cast<void*>(&ProxySocketOpenWriter),
            &original, "socket-trace writer")) {
        LogLine("socket-trace writer hook failed; parser and panel hooks remain passive");
        return false;
    }
    g_originalSocketOpenWriter = reinterpret_cast<SocketOpenWriterFn>(original);

    original = nullptr;
    if (!InstallKnownInlineHook(socketOp14ExtensionGateTarget, kSocketOp14ExtensionGatePrologue,
            sizeof(kSocketOp14ExtensionGatePrologue), reinterpret_cast<void*>(&ProxySocketOp14ExtensionGate),
            &original, "socket-trace op14/list3 extension gate")) {
        LogLine("socket-trace extension-gate hook failed; existing row/panel/writer hooks remain passive");
        return false;
    }
    g_originalSocketOp14ExtensionGate = reinterpret_cast<SocketOp14ExtensionGateFn>(original);
    LogLine("socket-trace installed row=0x%08X panel=0x%08X writer=0x%08X gate=0x%08X wait_ms=%d max_selections=%ld max_writer_hits=%ld max_gate_hits=%ld",
        itemRowParseTarget, panelSelectTarget, socketWriterTarget, socketOp14ExtensionGateTarget, waitedMs,
        kSocketTraceMaxSelections, kSocketTraceMaxWriterHits, kSocketTraceMaxExtensionGateHits);
    return true;
}

bool InstallContractUseCompatibility()
{
    static const unsigned char kInventoryUseUIAPrologue[] = {
        0x55, 0x8B, 0xEC, 0x6A, 0xFF, 0x68, 0x48, 0xC2, 0xCA, 0x03
    };
    static const unsigned char kInventoryUseUIBPrologue[] = {
        0x55, 0x8B, 0xEC, 0x6A, 0xFF, 0x68, 0xF9, 0xCD, 0xD3, 0x03
    };
    // push ebp; mov ebp,esp; mov eax,[ebp+0C]; mov edx,[ebp+10];
    // push esi; mov esi,ecx.
    static const unsigned char kInventoryUseGateAPrologue[] = {
        0x55, 0x8B, 0xEC, 0x8B, 0x45, 0x0C, 0x8B, 0x55, 0x10,
        0x56, 0x8B, 0xF1
    };
    // push ebp; mov ebp,esp; push esi; push 0x7D4; mov esi,ecx.
    static const unsigned char kInventoryUseGateBPrologue[] = {
        0x55, 0x8B, 0xEC, 0x56, 0x68, 0xD4, 0x07, 0x00, 0x00,
        0x8B, 0xF1
    };
    // push ebp; mov ebp,esp; push ecx; push ebx; push edi; mov edi,ecx.
    // This is the whole-instruction prefix of sub_2331CE0, whose `ret 8`
    // proves its (template ID, slot) stack arguments.
    static const unsigned char kInventoryUsePanelPrologue[] = {
        0x55, 0x8B, 0xEC, 0x51, 0x53, 0x57, 0x8B, 0xF9
    };
    static const unsigned char kSelectionCtorPrologue[] = {
        0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x10
    };
    static const unsigned char kSelectionCollectPrologue[] = {
        0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x14
    };
    static const unsigned char kSelectionDtorPrologue[] = {
        0x56, 0x8B, 0xF1, 0x8B, 0x46, 0x04
    };
    static const unsigned char kSelectionLookupPrologue[] = {
        0x55, 0x8B, 0xEC, 0x51, 0x56
    };
    static const unsigned char kTemplateReadPrologue[] = {
        0x56, 0x6A, 0x01, 0x6A, 0x32
    };
    static const unsigned char kIdentityReadPrologue[] = {
        0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x1C
    };
    static const unsigned char kWriterValue[] = {
        0xA1, 0xBC, 0xA4, 0x2E, 0x05, 0xC3
    };
    static const unsigned char kWriterU16Prologue[] = {
        0x55, 0x8B, 0xEC, 0x6A, 0xFF
    };
    static const unsigned char kWriterScalarPrologue[] = {
        0x55, 0x8B, 0xEC, 0x80, 0x79, 0x08, 0x00
    };
    static const unsigned char kWriterFlushPrologue[] = {
        0x55, 0x8B, 0xEC, 0xB8, 0x50, 0x00, 0x04, 0x00
    };

    uintptr_t uiATarget = g_dnfBase + kInventoryUseUIARva;
    uintptr_t uiBTarget = g_dnfBase + kInventoryUseUIBRva;
    uintptr_t gateATarget = g_dnfBase + kInventoryUseGateARva;
    uintptr_t gateBTarget = g_dnfBase + kInventoryUseGateBRva;
    uintptr_t panelTarget = g_dnfBase + kInventoryUsePanelRva;
    uintptr_t selectionCtorTarget = g_dnfBase + kInventorySelectionCtorRva;
    uintptr_t selectionCollectTarget = g_dnfBase + kInventorySelectionCollectRva;
    uintptr_t selectionDtorTarget = g_dnfBase + kInventorySelectionDtorRva;
    uintptr_t selectionLookupTarget = g_dnfBase + kInventorySelectionLookupRva;
    uintptr_t templateReadTarget = g_dnfBase + kCurrentItemTemplateReadRva;
    uintptr_t identityReadTarget = g_dnfBase + kCurrentItemIdentityReadRva;
    uintptr_t writerValueTarget = g_dnfBase + kUpperWriterValueRva;
    uintptr_t writerU16Target = g_dnfBase + kUpperWriterU16Rva;
    uintptr_t writerU8Target = g_dnfBase + kUpperWriterU8Rva;
    uintptr_t writerI16Target = g_dnfBase + kUpperWriterI16Rva;
    uintptr_t writerU32Target = g_dnfBase + kUpperWriterU32Rva;
    uintptr_t writerFlushTarget = g_dnfBase + kUpperWriterFlushRva;

    constexpr int kWaitLimitMs = 120000;
    int waitedMs = 0;
    while (waitedMs < kWaitLimitMs) {
        bool ready =
            BytesMatch(reinterpret_cast<unsigned char*>(uiATarget),
                kInventoryUseUIAPrologue, sizeof(kInventoryUseUIAPrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(uiBTarget),
                kInventoryUseUIBPrologue, sizeof(kInventoryUseUIBPrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(gateATarget),
                kInventoryUseGateAPrologue, sizeof(kInventoryUseGateAPrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(gateBTarget),
                kInventoryUseGateBPrologue, sizeof(kInventoryUseGateBPrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(panelTarget),
                kInventoryUsePanelPrologue, sizeof(kInventoryUsePanelPrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(selectionCtorTarget),
                kSelectionCtorPrologue, sizeof(kSelectionCtorPrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(selectionCollectTarget),
                kSelectionCollectPrologue, sizeof(kSelectionCollectPrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(selectionDtorTarget),
                kSelectionDtorPrologue, sizeof(kSelectionDtorPrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(selectionLookupTarget),
                kSelectionLookupPrologue, sizeof(kSelectionLookupPrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(templateReadTarget),
                kTemplateReadPrologue, sizeof(kTemplateReadPrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(identityReadTarget),
                kIdentityReadPrologue, sizeof(kIdentityReadPrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(writerValueTarget),
                kWriterValue, sizeof(kWriterValue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(writerU16Target),
                kWriterU16Prologue, sizeof(kWriterU16Prologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(writerU8Target),
                kWriterScalarPrologue, sizeof(kWriterScalarPrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(writerI16Target),
                kWriterScalarPrologue, sizeof(kWriterScalarPrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(writerU32Target),
                kWriterScalarPrologue, sizeof(kWriterScalarPrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(writerFlushTarget),
                kWriterFlushPrologue, sizeof(kWriterFlushPrologue));
        if (ready) break;
        Sleep(250);
        waitedMs += 250;
    }
    if (waitedMs >= kWaitLimitMs) {
        LogLine("contract-use compatibility targets did not reach audited bytes; no bypass installed uiA=0x%08X uiB=0x%08X gateA=0x%08X gateB=0x%08X panel=0x%08X",
            uiATarget, uiBTarget, gateATarget, gateBTarget, panelTarget);
        return false;
    }

    if (!InstallContractUseWindowProc()) {
        return false;
    }

    void* original = nullptr;
    if (!InstallKnownInlineHook(uiATarget, kInventoryUseUIAPrologue,
            sizeof(kInventoryUseUIAPrologue),
            reinterpret_cast<void*>(&ProxyInventoryUseUIA), &original,
            "contract-use main inventory callback A")) {
        return false;
    }
    g_originalInventoryUseUIA = original;

    original = nullptr;
    if (!InstallKnownInlineHook(uiBTarget, kInventoryUseUIBPrologue,
            sizeof(kInventoryUseUIBPrologue),
            reinterpret_cast<void*>(&ProxyInventoryUseUIB), &original,
            "contract-use main inventory callback B")) {
        return false;
    }
    g_originalInventoryUseUIB = original;

    original = nullptr;
    if (!InstallKnownInlineHook(gateATarget, kInventoryUseGateAPrologue,
            sizeof(kInventoryUseGateAPrologue),
            reinterpret_cast<void*>(&ProxyInventoryUseGateA), &original,
            "contract-use pre-panel gate A")) {
        return false;
    }
    g_originalInventoryUseGateA = original;

    original = nullptr;
    if (!InstallKnownInlineHook(gateBTarget, kInventoryUseGateBPrologue,
            sizeof(kInventoryUseGateBPrologue),
            reinterpret_cast<void*>(&ProxyInventoryUseGateB), &original,
            "contract-use pre-panel gate B")) {
        return false;
    }
    g_originalInventoryUseGateB = original;

    original = nullptr;
    if (!InstallKnownInlineHook(panelTarget, kInventoryUsePanelPrologue,
            sizeof(kInventoryUsePanelPrologue),
            reinterpret_cast<void*>(&ProxyInventoryUsePanel), &original,
            "contract-use active-coupon panel callback")) {
        return false;
    }
    g_originalInventoryUsePanel = original;

    LogLine("contract-use compatibility installed uiA=0x%08X uiB=0x%08X gateA=0x%08X gateB=0x%08X panel=0x%08X lookup=0x%08X template=0x%08X identity=0x%08X wait_ms=%d scope=premiumlist-only-native-container-scan",
        uiATarget, uiBTarget, gateATarget, gateBTarget, panelTarget,
        selectionLookupTarget, templateReadTarget, identityReadTarget, waitedMs);
    return true;
}

bool InstallPremiumStateCompatibility()
{
    if (!g_dnfBase) return false;
    uintptr_t class1DispatchTarget = g_dnfBase + kClass1DispatchRva;
    uintptr_t sceneUiOpenTarget = g_dnfBase + kSceneUiOpenRva;
    uintptr_t joustSceneBlockCheckTarget = g_dnfBase + kJoustSceneBlockCheckRva;
    uintptr_t joustHistoricTimeGateTarget = g_dnfBase + kJoustHistoricTimeGateRva;
    static const unsigned char kClass1DispatchPrologue[] = {
        0x55, 0x8B, 0xEC, 0x8B, 0x45, 0x08
    };
    static const unsigned char kSceneUiOpenPrologue[] = {
        0x55, 0x8B, 0xEC, 0x6A, 0xFF
    };
    static const unsigned char kJoustSceneBlockCheckPrologue[] = {
        0x8B, 0x81, 0xC0, 0x0F, 0x00, 0x00
    };
    static const unsigned char kJoustHistoricTimeGate[] = { 0x75, 0x34 };
    static const unsigned char kJoustHistoricTimeGatePatch[] = { 0xEB, 0x34 };

    constexpr int kWaitLimitMs = 120000;
    int waitedMs = 0;
    while (waitedMs < kWaitLimitMs) {
        if (BytesMatch(reinterpret_cast<unsigned char*>(class1DispatchTarget),
                kClass1DispatchPrologue, sizeof(kClass1DispatchPrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(sceneUiOpenTarget),
                kSceneUiOpenPrologue, sizeof(kSceneUiOpenPrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(joustSceneBlockCheckTarget),
                kJoustSceneBlockCheckPrologue,
                sizeof(kJoustSceneBlockCheckPrologue)) &&
            BytesMatch(reinterpret_cast<unsigned char*>(joustHistoricTimeGateTarget),
                kJoustHistoricTimeGate, sizeof(kJoustHistoricTimeGate))) {
            break;
        }
        Sleep(250);
        waitedMs += 250;
    }
    if (waitedMs >= kWaitLimitMs) {
        LogLine("premium-state compatibility targets unavailable class1=0x%08X scene_ui=0x%08X joust_scene_gate=0x%08X joust_time_gate=0x%08X",
            class1DispatchTarget, sceneUiOpenTarget, joustSceneBlockCheckTarget,
            joustHistoricTimeGateTarget);
        return false;
    }

    void* original = nullptr;
    if (!InstallKnownInlineHook(class1DispatchTarget,
            kClass1DispatchPrologue, sizeof(kClass1DispatchPrologue),
            reinterpret_cast<void*>(&ProxyClass1Dispatch), &original,
            "aura state class1 cache")) {
        return false;
    }
    g_originalClass1Dispatch = original;

    original = nullptr;
    if (!InstallKnownInlineHook(sceneUiOpenTarget,
            kSceneUiOpenPrologue, sizeof(kSceneUiOpenPrologue),
            reinterpret_cast<void*>(&ProxyAuraSkinSceneUiOpen), &original,
            "aura state deferred avatar panel apply")) {
        return false;
    }
    g_originalSceneUiOpen = original;

    original = nullptr;
    if (!InstallKnownInlineHook(joustSceneBlockCheckTarget,
            kJoustSceneBlockCheckPrologue,
            sizeof(kJoustSceneBlockCheckPrologue),
            reinterpret_cast<void*>(&ProxyJoustSceneBlockCheck), &original,
            "joust owner609 NPC-dialog scene gate")) {
        return false;
    }
    g_originalJoustSceneBlockCheck = original;
    if (!WriteCodePatch(joustHistoricTimeGateTarget,
            kJoustHistoricTimeGatePatch, sizeof(kJoustHistoricTimeGatePatch),
            "joust retired weekend time gate")) {
        return false;
    }
    LogLine("premium-state compatibility installed class1=0x%08X scene_ui=0x%08X joust_scene_gate=0x%08X joust_time_gate=0x%08X joust_caller=+0x%08X wait_ms=%d scope=native-op898-marked-op863-owner609-dialog-and-all-day-joust-gates",
        class1DispatchTarget, sceneUiOpenTarget, joustSceneBlockCheckTarget,
        joustHistoricTimeGateTarget,
        static_cast<unsigned int>(kJoustOpenGateReturnRva), waitedMs);
    return true;
}

bool InstallTownCopresenceCompatibility()
{
    uintptr_t actorLookupTarget = g_dnfBase + kActorByObjectKeyRva;
    uintptr_t op24SceneModeTarget = g_dnfBase + kOp24SceneModeRva;
    uintptr_t op24LoadingGateTarget = g_dnfBase + kOp24LoadingGateRva;
    // push ebp; mov ebp,esp; mov eax,[dword_51B2728]. All eight bytes are
    // complete instructions and the absolute load is safe in the trampoline.
    static const unsigned char kActorByObjectKeyPrologue[] = {
        0x55, 0x8B, 0xEC, 0xA1, 0x28, 0x27, 0x1B, 0x05
    };
    static const unsigned char kOp24SceneModePrologue[] = {
        0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x08
    };
    static const unsigned char kOp24LoadingGatePrologue[] = {
        0x55, 0x8B, 0xEC, 0x6A, 0xFF
    };
    if (!BytesMatch(reinterpret_cast<unsigned char*>(op24SceneModeTarget),
            kOp24SceneModePrologue, sizeof(kOp24SceneModePrologue)) ||
        !BytesMatch(reinterpret_cast<unsigned char*>(op24LoadingGateTarget),
            kOp24LoadingGatePrologue, sizeof(kOp24LoadingGatePrologue))) {
        LogLine("town lifecycle op24 gate signature mismatch scene=0x%08X loading=0x%08X",
            op24SceneModeTarget, op24LoadingGateTarget);
        return false;
    }
    void* original = nullptr;
    if (!InstallKnownInlineHook(
            actorLookupTarget,
            kActorByObjectKeyPrologue,
            sizeof(kActorByObjectKeyPrologue),
            reinterpret_cast<void*>(&ProxyTownActorByObjectKey),
            &original,
            "town co-presence scoped actor-context lookup")) {
        return false;
    }
    g_originalActorByObjectKey = original;

    original = nullptr;
    if (!InstallKnownInlineHook(
            op24SceneModeTarget,
            kOp24SceneModePrologue,
            sizeof(kOp24SceneModePrologue),
            reinterpret_cast<void*>(&ProxyOp24SceneMode),
            &original,
            "town lifecycle op24 scene-mode gate")) {
        return false;
    }
    g_originalOp24SceneMode = original;

    original = nullptr;
    if (!InstallKnownInlineHook(
            op24LoadingGateTarget,
            kOp24LoadingGatePrologue,
            sizeof(kOp24LoadingGatePrologue),
            reinterpret_cast<void*>(&ProxyOp24LoadingGate),
            &original,
            "town lifecycle op24 loading gate")) {
        return false;
    }
    g_originalOp24LoadingGate = original;

    uintptr_t target = g_dnfBase + kClass0DispatchRva;
    static const unsigned char kClass0DispatchPrologue[] = {
        0x55, 0x8B, 0xEC, 0x53, 0x8B, 0x5D, 0x0C
    };
    original = nullptr;
    if (!InstallKnownInlineHook(
            target,
            kClass0DispatchPrologue,
            sizeof(kClass0DispatchPrologue),
            reinterpret_cast<void*>(&ProxyTownCopresenceClass0Dispatch),
            &original,
            "town co-presence class0 owner-context bridge")) {
        return false;
    }
    g_originalClass0Dispatch = original;

    uintptr_t mode0OwnerCompareTarget = g_dnfBase + kMode0OwnerCompareRva;
    static const unsigned char kMode0OwnerCompare[] = {
        0x3B, 0xF1, 0x74, 0x42, 0x0F, 0xB6, 0xD0
    };
    if (!BytesMatch(reinterpret_cast<unsigned char*>(mode0OwnerCompareTarget),
            kMode0OwnerCompare, sizeof(kMode0OwnerCompare))) {
        LogLine("town co-presence mode0 owner compare signature mismatch target=0x%08X",
            mode0OwnerCompareTarget);
        return false;
    }

    g_mode0OwnerRemoteResume = g_dnfBase + kMode0OwnerRemoteRva;
    g_mode0OwnerLocalResume = g_dnfBase + kMode0OwnerLocalRva;
    unsigned char mode0OwnerComparePatch[7] = { 0xE9 };
    intptr_t relative = reinterpret_cast<intptr_t>(&ProxyMode0OwnerCompare) -
        static_cast<intptr_t>(mode0OwnerCompareTarget + 5);
    *reinterpret_cast<int32_t*>(mode0OwnerComparePatch + 1) =
        static_cast<int32_t>(relative);
    mode0OwnerComparePatch[5] = 0x90;
    mode0OwnerComparePatch[6] = 0x90;
    if (!WriteCodePatch(mode0OwnerCompareTarget, mode0OwnerComparePatch,
            sizeof(mode0OwnerComparePatch), "town mode0 current-scene owner compare")) {
        return false;
    }

    LogLine("town co-presence compatibility installed class0=0x%08X actor_lookup=0x%08X mode0_owner=0x%08X op24_scene_gate=0x%08X op24_loading_gate=0x%08X scope=context-fallback-native-visual-interaction-current-actor-and-lua-enter-town",
        target, actorLookupTarget, mode0OwnerCompareTarget,
        op24SceneModeTarget, op24LoadingGateTarget);
    return true;
}

bool InstallDungeonPickupCompatibility()
{
    if (!g_dnfBase) return false;

    uintptr_t manualPickupTarget = g_dnfBase + kManualPickupRva;
    static const unsigned char kManualPickupPrologue[] = {
        0x55, 0x8B, 0xEC, 0x81, 0xEC, 0x24, 0x02, 0x00, 0x00
    };

    constexpr int kWaitLimitMs = 120000;
    int waitedMs = 0;
    while (waitedMs < kWaitLimitMs) {
        if (BytesMatch(reinterpret_cast<unsigned char*>(manualPickupTarget),
                kManualPickupPrologue, sizeof(kManualPickupPrologue))) {
            break;
        }
        Sleep(250);
        waitedMs += 250;
    }
    if (waitedMs >= kWaitLimitMs) {
        LogLine("dungeon pickup compatibility target unavailable manual=0x%08X",
            manualPickupTarget);
        return false;
    }

    void* original = nullptr;
    if (!InstallKnownInlineHook(manualPickupTarget,
            kManualPickupPrologue, sizeof(kManualPickupPrologue),
            reinterpret_cast<void*>(&ProxyManualPickup), &original,
            "dungeon manual pickup container repair")) {
        return false;
    }
    g_originalManualPickup = original;

    LogLine("dungeon pickup compatibility installed manual=0x%08X native_ctor=0x%08X wait_ms=%d scope=manual-pickup-controlled-actor-null-container-only",
        manualPickupTarget,
        g_dnfBase + kPickupContainerConstructorRva, waitedMs);
    return true;
}

bool InstallCreatureRenameCompatibility()
{
    if (!g_dnfBase) return false;

    uintptr_t target = g_dnfBase + kCreatureRenameMapNullCheckRva;
    static const unsigned char kExpected[] = {
        0x83, 0xC4, 0x04, 0x8D, 0x48, 0x14, 0xE8
    };

    constexpr int kWaitLimitMs = 120000;
    int waitedMs = 0;
    while (waitedMs < kWaitLimitMs) {
        if (BytesMatch(reinterpret_cast<unsigned char*>(target),
                kExpected, sizeof(kExpected))) {
            break;
        }
        Sleep(250);
        waitedMs += 250;
    }
    if (waitedMs >= kWaitLimitMs) {
        LogLine("creature rename compatibility target unavailable null_guard=0x%08X",
            target);
        return false;
    }

    g_creatureRenameMapUpdateResume =
        g_dnfBase + kCreatureRenameMapUpdateRva;
    g_creatureRenameMapDoneResume =
        g_dnfBase + kCreatureRenameMapDoneRva;

    unsigned char patch[5] = { 0xE9 };
    intptr_t relative =
        reinterpret_cast<intptr_t>(&ProxyCreatureRenameMapNullCheck) -
        static_cast<intptr_t>(target + 5);
    *reinterpret_cast<int32_t*>(patch + 1) = static_cast<int32_t>(relative);
    if (!WriteCodePatch(target, patch, sizeof(patch),
            "creature rename map null guard")) {
        return false;
    }

    LogLine("creature rename compatibility installed null_guard=0x%08X native_update=0x%08X native_done=0x%08X wait_ms=%d scope=optional-op105-map-copy-only",
        target, g_creatureRenameMapUpdateResume,
        g_creatureRenameMapDoneResume, waitedMs);
    return true;
}

bool InstallPetEnchantDisplayCompatibility()
{
    if (!g_dnfBase) return false;

    uintptr_t target = g_dnfBase + kPetItemUpdateDynamicStateRva;
    static const unsigned char kExpected[] = {
        0x0F, 0xBF, 0x85, 0x84, 0xFE, 0xFF, 0xFF
    };

    constexpr int kWaitLimitMs = 120000;
    int waitedMs = 0;
    while (waitedMs < kWaitLimitMs) {
        if (BytesMatch(reinterpret_cast<unsigned char*>(target),
                kExpected, sizeof(kExpected))) {
            break;
        }
        Sleep(250);
        waitedMs += 250;
    }
    if (waitedMs >= kWaitLimitMs) {
        LogLine("pet enchant display compatibility target unavailable same_item=0x%08X",
            target);
        return false;
    }

    g_petItemUpdateDynamicStateResume =
        g_dnfBase + kPetItemUpdateDynamicStateResumeRva;
    unsigned char patch[7] = { 0xE9 };
    intptr_t relative =
        reinterpret_cast<intptr_t>(&ProxyPetItemUpdateDynamicState) -
        static_cast<intptr_t>(target + 5);
    *reinterpret_cast<int32_t*>(patch + 1) = static_cast<int32_t>(relative);
    patch[5] = 0x90;
    patch[6] = 0x90;
    if (!WriteCodePatch(target, patch, sizeof(patch),
            "pet list-7 or equipped slot-26 op14 dynamic-state refresh")) {
        return false;
    }

    LogLine("pet enchant display compatibility installed same_item=0x%08X native_resume=0x%08X wait_ms=%d scope=list7-or-list3-slot26-same-template-op14-native-dynamic-state",
        target, g_petItemUpdateDynamicStateResume, waitedMs);
    return true;
}

// Historical research installer retained as non-building evidence only. It
// mixed UI, scene, actor, inventory, quest, contract, and debugger behavior
// into the transport DLL. Production must never compile or call this path.
#if 0
DWORD WINAPI InstallWorkerInner(void*)
{
    g_dnfBase = reinterpret_cast<uintptr_t>(GetModuleHandleW(nullptr));
    if (!g_exceptionTraceHandle) {
        g_exceptionTraceHandle = AddVectoredExceptionHandler(1, TraceUnhandledClientException);
    }
    LogLine("90CN trace enabled exception_handler=%p module_base=0x%08X",
        g_exceptionTraceHandle, g_dnfBase);
    InstallEmbeddedDebugCompatibility();
    LogProtocolPacket("LOGGER", 0, nullptr, 0);

    g_checksumFn = g_dnfBase + kChecksumRva;
    g_gameAllocatorFn = g_dnfBase + kGameAllocatorRva;
    g_gameMemcpyFn = g_dnfBase + kGameMemcpyRva;
    g_dprotoOutgoingResume = g_dnfBase + kDprotoOutgoingResumeRva;
    g_dprotoOutgoingReturn = g_dnfBase + kDprotoOutgoingReturnRva;
    g_dprotoFalseResume = g_dnfBase + kDprotoFalseResumeRva;
    g_mode0OwnerRemoteResume = g_dnfBase + kMode0OwnerRemoteRva;
    g_mode0OwnerLocalResume = g_dnfBase + kMode0OwnerLocalRva;
    g_mode3OwnerRemoteResume = g_dnfBase + kMode3OwnerRemoteResumeRva;
    g_mode3OwnerLocalResume = g_dnfBase + kMode3OwnerLocalResumeRva;
    g_mode3OwnerFinalizeRemoteResume =
        g_dnfBase + kMode3OwnerFinalizeRemoteResumeRva;
    g_mode3OwnerFinalizeLocalResume =
        g_dnfBase + kMode3OwnerFinalizeLocalResumeRva;
    g_mode3LocalOwnerChannelAddress = g_dnfBase + kLocalOwnerChannelRva;
    g_creatureRenameMapUpdateResume = g_dnfBase + kCreatureRenameMapUpdateRva;
    g_creatureRenameMapDoneResume = g_dnfBase + kCreatureRenameMapDoneRva;
    g_autoRepairAuxLookupFn = g_dnfBase + kInventorySelectionLookupRva;
    g_autoRepairAuxLookupResume = g_dnfBase + kAutoRepairAuxLookupResumeRva;
    g_autoRepairAuxLoopDone = g_dnfBase + kAutoRepairAuxLoopDoneRva;
    g_questAutoCompleteSendFn = g_dnfBase + kQuestAutoCompleteSendRva;

    uintptr_t cipherTarget = g_dnfBase + kCipherEncodeRva;
    uintptr_t cipherDecodeTarget = g_dnfBase + kCipherDecodeRva;
    uintptr_t cipherCallTarget = g_dnfBase + kCipherEncodeCallRva;
    uintptr_t routePredicateTarget = g_dnfBase + kDprotoRoutePredicateRva;
    uintptr_t outgoingGateTarget = g_dnfBase + kDprotoOutgoingGateRva;
    uintptr_t outgoingCloneTarget = g_dnfBase + kDprotoOutgoingCloneRva;
    uintptr_t selectedPageApplyTarget = g_dnfBase + kSelectedPageApplyRva;
    uintptr_t selectorCreateTickTarget = g_dnfBase + kSelectorCreateTickRva;
    uintptr_t selectorCreateTransitionTarget =
        g_dnfBase + kSelectorCreateTransitionRva;
    uintptr_t createUIClickTarget = g_dnfBase + kCreateUIClickRva;
    uintptr_t createUIOpenTarget = g_dnfBase + kCreateUIOpenRva;
    uintptr_t upperCreateSendTarget = g_dnfBase + kUpperCreateSendRva;
    uintptr_t class0DispatchTarget = g_dnfBase + kClass0DispatchRva;
    uintptr_t class1DispatchTarget = g_dnfBase + kClass1DispatchRva;
    uintptr_t sceneUiOpenTarget = g_dnfBase + kSceneUiOpenRva;
    uintptr_t localActorCreateTarget = g_dnfBase + kLocalActorCreateRva;
    uintptr_t mode0OwnerCompareTarget = g_dnfBase + kMode0OwnerCompareRva;
    uintptr_t mode3OwnerResolveTarget = g_dnfBase + kMode3OwnerResolveRva;
    uintptr_t mode3OwnerFinalizeTarget = g_dnfBase + kMode3OwnerFinalizeRva;
    uintptr_t pickupContainerGetterTarget =
        g_dnfBase + kPickupContainerGetterRva;
    uintptr_t manualPickupTarget = g_dnfBase + kManualPickupRva;
    uintptr_t creatureRenameMapNullCheckTarget =
        g_dnfBase + kCreatureRenameMapNullCheckRva;
    uintptr_t autoRepairAuxLookupTarget =
        g_dnfBase + kAutoRepairAuxLookupRva;
    uintptr_t op24SceneModeTarget = g_dnfBase + kOp24SceneModeRva;
    uintptr_t op24LoadingGateTarget = g_dnfBase + kOp24LoadingGateRva;
    uintptr_t questAutoCompleteCallContext = g_dnfBase + kQuestAutoCompleteCallContextRva;
    uintptr_t questAutoCompleteCallTarget = g_dnfBase + kQuestAutoCompleteCallRva;
    uintptr_t questAutoCompleteCaptureTarget = g_dnfBase + kQuestAutoCompleteCaptureRva;
    // The op1128 handler's unconditional trailing toast uses legacy string
    // ids that this build maps to an unrelated cross-class purchase warning.
    // The native result window already owns the complete ten-open result.
    uintptr_t magicBoxFollowupToastTarget = g_dnfBase + 0x019081CF;
    static const unsigned char kNativePrologue[] = { 0x55, 0x8B, 0xEC, 0x53, 0x8B, 0x5D };
    static const unsigned char kSelectedPageApplyPrologue[] = { 0x55, 0x8B, 0xEC, 0x6A, 0xFF };
    static const unsigned char kSelectorCreateTickPrologue[] = {
        0x56, 0x8B, 0xF1, 0x8B, 0x0D, 0x64, 0x27, 0x1B, 0x05
    };
    static const unsigned char kSelectorCreateTransitionPrologue[] = {
        0x56, 0x57, 0x8B, 0xF9, 0x8B, 0xB7, 0xA8, 0x00, 0x00, 0x00
    };
    static const unsigned char kCreateUIClickPrologue[] = {
        0x55, 0x8B, 0xEC, 0x51, 0x53, 0x56, 0x57
    };
    static const unsigned char kCreateUIOpenPrologue[] = { 0x55, 0x8B, 0xEC, 0x6A, 0xFF };
    static const unsigned char kUpperCreateSendPrologue[] = { 0x55, 0x8B, 0xEC, 0x51, 0x56 };
    static const unsigned char kClass0DispatchPrologue[] = { 0x55, 0x8B, 0xEC, 0x53, 0x8B, 0x5D, 0x0C };
    static const unsigned char kClass1DispatchPrologue[] = { 0x55, 0x8B, 0xEC, 0x8B, 0x45, 0x08 };
    static const unsigned char kSceneUiOpenPrologue[] = { 0x55, 0x8B, 0xEC, 0x6A, 0xFF };
    static const unsigned char kLocalActorCreatePrologue[] = { 0x55, 0x8B, 0xEC, 0x51, 0x57 };
    static const unsigned char kMode0OwnerCompare[] = { 0x3B, 0xF1, 0x74, 0x42, 0x0F, 0xB6, 0xD0 };
    static const unsigned char kMode3OwnerResolve[] = {
        0x3B, 0xCE, 0x74, 0x3F, 0x8B, 0xB5, 0x50, 0xFB, 0xFF, 0xFF
    };
    static const unsigned char kMode3OwnerFinalize[] = {
        0x0F, 0xB6, 0x05, 0x49, 0x9C, 0x19, 0x05, 0x3B, 0xC3, 0x74, 0x09
    };
    static const unsigned char kPickupContainerGetterPrologue[] = {
        0x8B, 0x81, 0x6C, 0x64, 0x00, 0x00
    };
    static const unsigned char kManualPickupPrologue[] = {
        0x55, 0x8B, 0xEC, 0x81, 0xEC, 0x24, 0x02, 0x00, 0x00
    };
    static const unsigned char kCreatureRenameMapNullCheck[] = {
        0x83, 0xC4, 0x04, 0x8D, 0x48, 0x14, 0xE8
    };
    static const unsigned char kAutoRepairAuxLookup[] = {
        0x8B, 0xC8, 0xE8, 0xDA, 0xD0, 0x2B, 0x00
    };
    static const unsigned char kOp24SceneModePrologue[] = { 0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x08 };
    static const unsigned char kOp24LoadingGatePrologue[] = { 0x55, 0x8B, 0xEC, 0x6A, 0xFF };
    static const unsigned char kCipherEncodeCall[] = { 0xE8, 0x3B, 0x46, 0xF5, 0xFF };
    static const unsigned char kRoutePredicatePrologue[] = { 0x55, 0x8B, 0xEC, 0x8B, 0x49, 0x0C };
    static const unsigned char kOutgoingGate[] = { 0x0F, 0x84, 0x2F, 0x01, 0x00, 0x00 };
    static const unsigned char kOutgoingClonePrologue[] = { 0x8B, 0x4B, 0x0C, 0x8B, 0x01 };
    static const unsigned char kQuestAutoCompleteCallContext[] = {
        0x8B, 0x0D, 0x98, 0x27, 0x1B, 0x05, 0x6A, 0xFF, 0x6A, 0x01,
        0xE8, 0x29, 0x07, 0x9A, 0xFE
    };
    static const unsigned char kQuestAutoCompleteCapture[] = { 0x89, 0x5E, 0x30 };
    static const unsigned char kMagicBoxFollowupToastCall[] = {
        0xE8, 0xEC, 0x77, 0x56, 0x00
    };

    LogLine("cipher worker started (module base 0x%08X)", g_dnfBase);
    InstallUiCompatibilityHook();
    InstallMultiClientCompatibilityHook();

    // TCLS launch argument fetches run before the first network connection.
    // Install only these six local helpers immediately.
    bool tclsInstalled = InstallTclsCompatibility();
    LogLine("TCLS local launch compatibility result=%d", tclsInstalled ? 1 : 0);

    bool channelDiagnosticInstalled = InstallChannelDiagnostic();
    LogLine("channel diagnostic result=%d", channelDiagnosticInstalled ? 1 : 0);
    bool channelUiCompatibilityInstalled = InstallChannelUiCompatibility();
    LogLine("channel UI compatibility result=%d",
        channelUiCompatibilityInstalled ? 1 : 0);

    LogLine("TCLS installed; continuing to install source-built sub_33AAFE0/sub_33AB0A0 upper-body plaintext bypass plus bounded scene route allow-list");

    wchar_t enabled[8] = { 0 };
    DWORD enabledLength = GetEnvironmentVariableW(L"DNF_CIPHER_PASSTHROUGH", enabled, 8);
    if (enabledLength > 0 && enabled[0] == L'0') {
        LogLine("DNF_CIPHER_PASSTHROUGH=0; hook disabled by configuration");
        return 0;
    }

    wchar_t dprotoEnabled[8] = { 0 };
    DWORD dprotoEnabledLength = GetEnvironmentVariableW(L"DNF_DPROTO_COMPAT", dprotoEnabled, 8);
    // DPROTO is not an ordinary body codec. The bridge must receive the
    // current EXE's finalized inner packets so dungeon combat, drops,
    // settlement, inventory, and UI routes can be handled by the server
    // without the external route/DPROTO PS1 script. This DLL ports the proven
    // PS1 shape: clone the predicate-true finalized packet, clone only the
    // measured predicate-false opcodes, and keep inbound route admission on the
    // bounded scene allow-list. DNF_DPROTO_COMPAT=0 restores the native wrapper.
    bool enableDprotoCompat = !(dprotoEnabledLength > 0 && dprotoEnabled[0] == L'0');

    wchar_t routeEnabled[8] = { 0 };
    DWORD routeEnabledLength = GetEnvironmentVariableW(L"DNF_ROUTE_COMPAT", routeEnabled, 8);
    // Keep the narrow inbound scene/UI route predicate enabled by default.
    // Set DNF_ROUTE_COMPAT=0 for rollback. This is not a send/recv hook and it
    // does not modify packet bytes; it only admits proven current-scene opcodes
    // to the native direct upper readers.
    bool enableSceneRouteCompat = routeEnabledLength == 0 || routeEnabled[0] != L'0';

    wchar_t questSingleTargetEnabled[8] = { 0 };
    DWORD questSingleTargetEnabledLength = GetEnvironmentVariableW(
        L"DNF_QUEST_SINGLE_TARGET", questSingleTargetEnabled, 8);
    // The bottom task-completion confirmation already captures the selected
    // row id. Enable the exact-row sender by default; set
    // DNF_QUEST_SINGLE_TARGET=0 for a clean rollback to the native u32(0).
    bool enableQuestSingleTarget = questSingleTargetEnabledLength == 0 ||
        questSingleTargetEnabled[0] != L'0';

    // Install as soon as the packed EXE has restored the verified native
    // prologues.  A fixed post-TCLS sleep misses the early GET_USERINFO path:
    // the client has already emitted the role-select handshake before the
    // plaintext upper-body proxy is active, so the server receives a legacy
    // packet stream and the selector renders black.  The byte-signature wait
    // below is the safety gate; do not add an unconditional startup delay here.
    constexpr int kBootstrapDelayMs = 0;
    if (kBootstrapDelayMs > 0) {
        Sleep(kBootstrapDelayMs);
    }

    // The packed EXE restores .text late. Refuse to patch unknown code.
    constexpr int kWaitLimitMs = 120000;
    int waitedMs = 0;
    while (waitedMs < kWaitLimitMs) {
        bool cipherReady = BytesMatch(reinterpret_cast<unsigned char*>(cipherTarget), kNativePrologue, sizeof(kNativePrologue));
        bool cipherDecodeReady = BytesMatch(reinterpret_cast<unsigned char*>(cipherDecodeTarget), kNativePrologue, sizeof(kNativePrologue));
        bool selectedPageApplyReady = BytesMatch(reinterpret_cast<unsigned char*>(selectedPageApplyTarget),
            kSelectedPageApplyPrologue, sizeof(kSelectedPageApplyPrologue));
        bool createUIClickReady = BytesMatch(reinterpret_cast<unsigned char*>(createUIClickTarget),
            kCreateUIClickPrologue, sizeof(kCreateUIClickPrologue));
        bool createUIOpenReady = BytesMatch(reinterpret_cast<unsigned char*>(createUIOpenTarget),
            kCreateUIOpenPrologue, sizeof(kCreateUIOpenPrologue));
        bool upperCreateSendReady = BytesMatch(reinterpret_cast<unsigned char*>(upperCreateSendTarget),
            kUpperCreateSendPrologue, sizeof(kUpperCreateSendPrologue));
        bool localActorCreateReady = BytesMatch(reinterpret_cast<unsigned char*>(localActorCreateTarget),
            kLocalActorCreatePrologue, sizeof(kLocalActorCreatePrologue));
        bool mode0OwnerCompareReady = BytesMatch(reinterpret_cast<unsigned char*>(mode0OwnerCompareTarget),
            kMode0OwnerCompare, sizeof(kMode0OwnerCompare));
        bool mode3OwnerResolveReady = BytesMatch(reinterpret_cast<unsigned char*>(mode3OwnerResolveTarget),
            kMode3OwnerResolve, sizeof(kMode3OwnerResolve));
        bool mode3OwnerFinalizeReady = BytesMatch(
            reinterpret_cast<unsigned char*>(mode3OwnerFinalizeTarget),
            kMode3OwnerFinalize, sizeof(kMode3OwnerFinalize));
        bool pickupContainerGetterReady = BytesMatch(
            reinterpret_cast<unsigned char*>(pickupContainerGetterTarget),
            kPickupContainerGetterPrologue, sizeof(kPickupContainerGetterPrologue));
        bool manualPickupReady = BytesMatch(
            reinterpret_cast<unsigned char*>(manualPickupTarget),
            kManualPickupPrologue, sizeof(kManualPickupPrologue));
        bool creatureRenameMapNullCheckReady = BytesMatch(
            reinterpret_cast<unsigned char*>(creatureRenameMapNullCheckTarget),
            kCreatureRenameMapNullCheck, sizeof(kCreatureRenameMapNullCheck));
        bool autoRepairAuxLookupReady = BytesMatch(
            reinterpret_cast<unsigned char*>(autoRepairAuxLookupTarget),
            kAutoRepairAuxLookup, sizeof(kAutoRepairAuxLookup));
        bool op24SceneModeReady = BytesMatch(reinterpret_cast<unsigned char*>(op24SceneModeTarget),
            kOp24SceneModePrologue, sizeof(kOp24SceneModePrologue));
        bool op24LoadingGateReady = BytesMatch(reinterpret_cast<unsigned char*>(op24LoadingGateTarget),
            kOp24LoadingGatePrologue, sizeof(kOp24LoadingGatePrologue));
        bool routeReady = !enableSceneRouteCompat ||
            BytesMatch(reinterpret_cast<unsigned char*>(routePredicateTarget), kRoutePredicatePrologue, sizeof(kRoutePredicatePrologue));
        bool dprotoReady = !enableDprotoCompat ||
            (BytesMatch(reinterpret_cast<unsigned char*>(routePredicateTarget), kRoutePredicatePrologue, sizeof(kRoutePredicatePrologue)) &&
             BytesMatch(reinterpret_cast<unsigned char*>(outgoingGateTarget), kOutgoingGate, sizeof(kOutgoingGate)) &&
             BytesMatch(reinterpret_cast<unsigned char*>(outgoingCloneTarget), kOutgoingClonePrologue, sizeof(kOutgoingClonePrologue)));
        bool questSingleTargetReady = !enableQuestSingleTarget ||
            (BytesMatch(reinterpret_cast<unsigned char*>(questAutoCompleteCallContext),
                kQuestAutoCompleteCallContext, sizeof(kQuestAutoCompleteCallContext)) &&
             BytesMatch(reinterpret_cast<unsigned char*>(questAutoCompleteCaptureTarget),
                kQuestAutoCompleteCapture, sizeof(kQuestAutoCompleteCapture)));
        bool magicBoxReady =
            BytesMatch(reinterpret_cast<unsigned char*>(magicBoxFollowupToastTarget),
                kMagicBoxFollowupToastCall, sizeof(kMagicBoxFollowupToastCall));
        if (cipherReady && cipherDecodeReady && selectedPageApplyReady &&
            createUIClickReady && createUIOpenReady && upperCreateSendReady &&
            localActorCreateReady &&
            mode0OwnerCompareReady && mode3OwnerResolveReady &&
            mode3OwnerFinalizeReady && pickupContainerGetterReady &&
            manualPickupReady &&
            creatureRenameMapNullCheckReady &&
            autoRepairAuxLookupReady &&
            op24SceneModeReady && op24LoadingGateReady &&
            routeReady && dprotoReady && questSingleTargetReady && magicBoxReady) break;

        unsigned char firstByte = 0;
        if (TryReadByte(reinterpret_cast<unsigned char*>(cipherTarget), &firstByte) && firstByte == 0xE9) {
            LogLine("cipher encode function already hooked; fresh DNF process required for this build");
            return 0;
        }
        if (TryReadByte(reinterpret_cast<unsigned char*>(cipherDecodeTarget), &firstByte) && firstByte == 0xE9) {
            LogLine("cipher decode function already hooked; fresh DNF process required for this build");
            return 0;
        }
        Sleep(250);
        waitedMs += 250;
    }
    if (waitedMs >= kWaitLimitMs) {
        LogLine("native targets did not reach verified bytes; hook not installed route=%d dproto=%d quest_single=%d",
            enableSceneRouteCompat ? 1 : 0, enableDprotoCompat ? 1 : 0,
            enableQuestSingleTarget ? 1 : 0);
        return 0;
    }

    unsigned char entryPatch[5] = { 0xE8 };
    entryPatch[0] = 0xE9;
    intptr_t relative = reinterpret_cast<intptr_t>(&ProxyCipherEncodeFunction) - static_cast<intptr_t>(cipherTarget + 5);
    *reinterpret_cast<int32_t*>(entryPatch + 1) = static_cast<int32_t>(relative);
    if (!WriteCodePatch(cipherTarget, entryPatch, sizeof(entryPatch), "upper body encode function")) return 0;

    unsigned char decodePatch[5] = { 0xE9 };
    relative = reinterpret_cast<intptr_t>(&ProxyCipherDecodeFunction) - static_cast<intptr_t>(cipherDecodeTarget + 5);
    *reinterpret_cast<int32_t*>(decodePatch + 1) = static_cast<int32_t>(relative);
    if (!WriteCodePatch(cipherDecodeTarget, decodePatch, sizeof(decodePatch), "upper body decode function")) return 0;

    if (enableSceneRouteCompat) {
        unsigned char routeAllowPatch[6] = { 0xE9 };
        intptr_t routeRelative = reinterpret_cast<intptr_t>(&ProxySceneRouteAllowList) - static_cast<intptr_t>(routePredicateTarget + 5);
        *reinterpret_cast<int32_t*>(routeAllowPatch + 1) = static_cast<int32_t>(routeRelative);
        routeAllowPatch[5] = 0x90;
        if (!WriteCodePatch(routePredicateTarget, routeAllowPatch, sizeof(routeAllowPatch), "scene route allow-list")) return 0;
    }

    if (enableDprotoCompat) {
        unsigned char clonePatch[5] = { 0xE9 };
        intptr_t cloneRelative = reinterpret_cast<intptr_t>(&ProxyDprotoSendDirect) - static_cast<intptr_t>(outgoingCloneTarget + 5);
        *reinterpret_cast<int32_t*>(clonePatch + 1) = static_cast<int32_t>(cloneRelative);

        unsigned char falsePatch[6] = { 0x0F, 0x84 };
        intptr_t falseRelative = reinterpret_cast<intptr_t>(&ProxyDprotoFalseSelective) - static_cast<intptr_t>(outgoingGateTarget + 6);
        *reinterpret_cast<int32_t*>(falsePatch + 2) = static_cast<int32_t>(falseRelative);

        if (!WriteCodePatch(outgoingCloneTarget, clonePatch, sizeof(clonePatch), "dproto outbound predicate-true clone") ||
            !WriteCodePatch(outgoingGateTarget, falsePatch, sizeof(falsePatch), "dproto outbound predicate-false selective")) {
            LogLine("DPROTO compatibility patch incomplete; fresh DNF process required");
            return 0;
        }
    }

    static const unsigned char kMagicBoxToastNops[] = {
        0x90, 0x90, 0x90, 0x90, 0x90
    };
    if (!WriteCodePatch(magicBoxFollowupToastTarget,
            kMagicBoxToastNops, sizeof(kMagicBoxToastNops),
            "seria ten-open stale follow-up toast")) {
        return 0;
    }
    LogLine("seria ten-open compatibility installed toast=0x%08X",
        magicBoxFollowupToastTarget);

    if (enableQuestSingleTarget) {
        unsigned char questSingleTargetPatch[5] = { 0xE8 };
        intptr_t questRelative = reinterpret_cast<intptr_t>(&ProxyQuestAutoCompleteSelected) -
            static_cast<intptr_t>(questAutoCompleteCallTarget + 5);
        *reinterpret_cast<int32_t*>(questSingleTargetPatch + 1) = static_cast<int32_t>(questRelative);
        if (!WriteCodePatch(questAutoCompleteCallTarget, questSingleTargetPatch,
                sizeof(questSingleTargetPatch), "quest selected single-target confirmation")) {
            LogLine("quest selected single-target patch failed; native zero-selector remains");
            return 0;
        }
        LogLine("quest selected single-target installed call=0x%08X capture=0x%08X native_send=0x%08X",
            questAutoCompleteCallTarget, questAutoCompleteCaptureTarget, g_questAutoCompleteSendFn);
    }

    unsigned char mode0OwnerComparePatch[7] = { 0xE9 };
    relative = reinterpret_cast<intptr_t>(&ProxyMode0OwnerCompare) -
        static_cast<intptr_t>(mode0OwnerCompareTarget + 5);
    *reinterpret_cast<int32_t*>(mode0OwnerComparePatch + 1) = static_cast<int32_t>(relative);
    mode0OwnerComparePatch[5] = 0x90;
    mode0OwnerComparePatch[6] = 0x90;
    if (!WriteCodePatch(mode0OwnerCompareTarget, mode0OwnerComparePatch,
            sizeof(mode0OwnerComparePatch), "mode0 owner compare diagnostic")) {
        return 0;
    }

    unsigned char mode3OwnerResolvePatch[10] = { 0xE9 };
    relative = reinterpret_cast<intptr_t>(&ProxyMode3OwnerResolve) -
        static_cast<intptr_t>(mode3OwnerResolveTarget + 5);
    *reinterpret_cast<int32_t*>(mode3OwnerResolvePatch + 1) =
        static_cast<int32_t>(relative);
    for (size_t i = 5; i < sizeof(mode3OwnerResolvePatch); ++i) {
        mode3OwnerResolvePatch[i] = 0x90;
    }
    if (!WriteCodePatch(mode3OwnerResolveTarget, mode3OwnerResolvePatch,
            sizeof(mode3OwnerResolvePatch), "mode3 current-scene owner resolve")) {
        return 0;
    }

    unsigned char mode3OwnerFinalizePatch[11] = { 0xE9 };
    relative = reinterpret_cast<intptr_t>(&ProxyMode3OwnerFinalize) -
        static_cast<intptr_t>(mode3OwnerFinalizeTarget + 5);
    *reinterpret_cast<int32_t*>(mode3OwnerFinalizePatch + 1) =
        static_cast<int32_t>(relative);
    for (size_t i = 5; i < sizeof(mode3OwnerFinalizePatch); ++i) {
        mode3OwnerFinalizePatch[i] = 0x90;
    }
    if (!WriteCodePatch(mode3OwnerFinalizeTarget, mode3OwnerFinalizePatch,
            sizeof(mode3OwnerFinalizePatch),
            "mode3 current-scene owner finalization")) {
        return 0;
    }
    LogLine("mode3 current-scene compatibility installed resolve=0x%08X finalize=0x%08X local=0x%08X remote=0x%08X",
        mode3OwnerResolveTarget, mode3OwnerFinalizeTarget,
        g_mode3OwnerFinalizeLocalResume, g_mode3OwnerFinalizeRemoteResume);

    void* pickupContainerGetterOriginal = nullptr;
    if (!InstallKnownInlineHook(pickupContainerGetterTarget,
            kPickupContainerGetterPrologue,
            sizeof(kPickupContainerGetterPrologue),
            reinterpret_cast<void*>(&ProxyPickupContainerGetter),
            &pickupContainerGetterOriginal,
            "dungeon pickup container getter repair")) {
        return 0;
    }
    g_originalPickupContainerGetter = pickupContainerGetterOriginal;

    void* manualPickupOriginal = nullptr;
    if (!InstallKnownInlineHook(manualPickupTarget,
            kManualPickupPrologue, sizeof(kManualPickupPrologue),
            reinterpret_cast<void*>(&ProxyManualPickup),
            &manualPickupOriginal,
            "dungeon manual pickup container repair")) {
        return 0;
    }
    g_originalManualPickup = manualPickupOriginal;
    LogLine("dungeon pickup compatibility installed getter=0x%08X manual=0x%08X native_ctor=0x%08X",
        pickupContainerGetterTarget, manualPickupTarget,
        g_dnfBase + kPickupContainerConstructorRva);

    unsigned char creatureRenameNullPatch[5] = { 0xE9 };
    relative = reinterpret_cast<intptr_t>(&ProxyCreatureRenameMapNullCheck) -
        static_cast<intptr_t>(creatureRenameMapNullCheckTarget + 5);
    *reinterpret_cast<int32_t*>(creatureRenameNullPatch + 1) =
        static_cast<int32_t>(relative);
    if (!WriteCodePatch(creatureRenameMapNullCheckTarget,
            creatureRenameNullPatch, sizeof(creatureRenameNullPatch),
            "creature rename map null guard")) {
        return 0;
    }
    LogLine("creature rename compatibility installed null_guard=0x%08X native_update=0x%08X",
        creatureRenameMapNullCheckTarget, g_creatureRenameMapUpdateResume);

    unsigned char autoRepairAuxNullPatch[7] = { 0xE9 };
    relative = reinterpret_cast<intptr_t>(&ProxyAutoRepairAuxContainerLookup) -
        static_cast<intptr_t>(autoRepairAuxLookupTarget + 5);
    *reinterpret_cast<int32_t*>(autoRepairAuxNullPatch + 1) =
        static_cast<int32_t>(relative);
    autoRepairAuxNullPatch[5] = 0x90;
    autoRepairAuxNullPatch[6] = 0x90;
    if (!WriteCodePatch(autoRepairAuxLookupTarget,
            autoRepairAuxNullPatch, sizeof(autoRepairAuxNullPatch),
            "op903 auto-repair auxiliary equipment null guard")) {
        return 0;
    }
    LogLine("auto-repair auxiliary equipment compatibility installed null_guard=0x%08X native_lookup=0x%08X",
        autoRepairAuxLookupTarget, g_autoRepairAuxLookupFn);

    void* selectedPageApplyOriginal = nullptr;
    if (!InstallKnownInlineHook(selectedPageApplyTarget,
            kSelectedPageApplyPrologue, sizeof(kSelectedPageApplyPrologue),
            reinterpret_cast<void*>(&ProxySelectedPageApply), &selectedPageApplyOriginal,
            "selected page null guard")) {
        return 0;
    }
    g_originalSelectedPageApply = selectedPageApplyOriginal;

    if (BytesMatch(reinterpret_cast<unsigned char*>(selectorCreateTickTarget),
            kSelectorCreateTickPrologue, sizeof(kSelectorCreateTickPrologue))) {
        void* selectorCreateTickOriginal = nullptr;
        if (InstallKnownInlineHook(selectorCreateTickTarget,
                kSelectorCreateTickPrologue, sizeof(kSelectorCreateTickPrologue),
                reinterpret_cast<void*>(&ProxySelectorCreateTick), &selectorCreateTickOriginal,
                "selector create gate trace")) {
            g_originalSelectorCreateTick = selectorCreateTickOriginal;
        } else {
            LogLine("selector create gate trace unavailable; compatibility remains active");
        }
    } else {
        LogLine("selector create gate trace skipped: prologue mismatch");
    }

    if (BytesMatch(reinterpret_cast<unsigned char*>(selectorCreateTransitionTarget),
            kSelectorCreateTransitionPrologue,
            sizeof(kSelectorCreateTransitionPrologue))) {
        void* selectorCreateTransitionOriginal = nullptr;
        if (InstallKnownInlineHook(selectorCreateTransitionTarget,
                kSelectorCreateTransitionPrologue,
                sizeof(kSelectorCreateTransitionPrologue),
                reinterpret_cast<void*>(&ProxySelectorCreateTransition),
                &selectorCreateTransitionOriginal,
                "selector create transition trace")) {
            g_originalSelectorCreateTransition = selectorCreateTransitionOriginal;
        } else {
            LogLine("selector create transition trace unavailable; compatibility remains active");
        }
    } else {
        LogLine("selector create transition trace skipped: prologue mismatch");
    }

    void* createUIClickOriginal = nullptr;
    if (!InstallKnownInlineHook(createUIClickTarget,
            kCreateUIClickPrologue, sizeof(kCreateUIClickPrologue),
            reinterpret_cast<void*>(&ProxyCreateUIClick), &createUIClickOriginal,
            "create UI click trace")) {
        return 0;
    }
    g_originalCreateUIClick = createUIClickOriginal;

    void* createUIOpenOriginal = nullptr;
    if (!InstallKnownInlineHook(createUIOpenTarget,
            kCreateUIOpenPrologue, sizeof(kCreateUIOpenPrologue),
            reinterpret_cast<void*>(&ProxyCreateUIOpen), &createUIOpenOriginal,
            "create UI open trace")) {
        return 0;
    }
    g_originalCreateUIOpen = createUIOpenOriginal;

    void* upperCreateSendOriginal = nullptr;
    if (!InstallKnownInlineHook(upperCreateSendTarget,
            kUpperCreateSendPrologue, sizeof(kUpperCreateSendPrologue),
            reinterpret_cast<void*>(&ProxyUpperCreateSend), &upperCreateSendOriginal,
            "upper create send trace")) {
        return 0;
    }
    g_originalUpperCreateSend = upperCreateSendOriginal;

    void* class0DispatchOriginal = nullptr;
    if (!InstallKnownInlineHook(class0DispatchTarget,
            kClass0DispatchPrologue, sizeof(kClass0DispatchPrologue),
            reinterpret_cast<void*>(&ProxyClass0Dispatch), &class0DispatchOriginal,
            "class0 dispatch diagnostic")) {
        return 0;
    }
    g_originalClass0Dispatch = class0DispatchOriginal;

    void* class1DispatchOriginal = nullptr;
    if (!InstallKnownInlineHook(class1DispatchTarget,
            kClass1DispatchPrologue, sizeof(kClass1DispatchPrologue),
            reinterpret_cast<void*>(&ProxyClass1Dispatch), &class1DispatchOriginal,
            "class1 dispatch diagnostic")) {
        return 0;
    }
    g_originalClass1Dispatch = class1DispatchOriginal;

    void* sceneUiOpenOriginal = nullptr;
    if (!InstallKnownInlineHook(sceneUiOpenTarget,
            kSceneUiOpenPrologue, sizeof(kSceneUiOpenPrologue),
            reinterpret_cast<void*>(&ProxySceneUiOpen), &sceneUiOpenOriginal,
            "aura skin deferred avatar panel unlock")) {
        return 0;
    }
    g_originalSceneUiOpen = sceneUiOpenOriginal;

    void* localActorCreateOriginal = nullptr;
    if (!InstallKnownInlineHook(localActorCreateTarget,
            kLocalActorCreatePrologue, sizeof(kLocalActorCreatePrologue),
            reinterpret_cast<void*>(&ProxyLocalActorCreate), &localActorCreateOriginal,
            "local actor create diagnostic")) {
        return 0;
    }
    g_originalLocalActorCreate = localActorCreateOriginal;

    void* op24SceneModeOriginal = nullptr;
    if (!InstallKnownInlineHook(op24SceneModeTarget,
            kOp24SceneModePrologue, sizeof(kOp24SceneModePrologue),
            reinterpret_cast<void*>(&ProxyOp24SceneMode), &op24SceneModeOriginal,
            "op24 scene mode diagnostic")) {
        return 0;
    }
    g_originalOp24SceneMode = op24SceneModeOriginal;

    void* op24LoadingGateOriginal = nullptr;
    if (!InstallKnownInlineHook(op24LoadingGateTarget,
            kOp24LoadingGatePrologue, sizeof(kOp24LoadingGatePrologue),
            reinterpret_cast<void*>(&ProxyOp24LoadingGate), &op24LoadingGateOriginal,
            "op24 loading gate diagnostic")) {
        return 0;
    }
    g_originalOp24LoadingGate = op24LoadingGateOriginal;

    // REQUEST_PEER/RESPONSE_PEER are the current EXE's town-player
    // interaction handshake. Log both registries before the server admits a
    // new response body so their concrete callbacks can be audited without
    // executing an unproved packet.
    TraceDispatchRegistryEntry(0, 10);
    TraceDispatchRegistryEntry(1, 10);
    TraceDispatchRegistryEntry(0, 11);
    TraceDispatchRegistryEntry(1, 11);

    LogLine("compatibility DLL installed upper_body_bypass=1 upper_decode_bypass=1 scene_route=%d encode_function=0x%08X decode_function=0x%08X dproto=%d dproto_true=0x%08X dproto_false=0x%08X route=0x%08X quest_single=%d quest_call=0x%08X bootstrap_delay_ms=%d native_wait_ms=%d",
        enableSceneRouteCompat ? 1 : 0, cipherTarget, cipherDecodeTarget,
        enableDprotoCompat ? 1 : 0, outgoingCloneTarget, outgoingGateTarget,
        routePredicateTarget, enableQuestSingleTarget ? 1 : 0,
        questAutoCompleteCallTarget, kBootstrapDelayMs, waitedMs);

    if (IsSocketTraceEnabled()) {
        bool socketTraceInstalled = InstallSocketOpenTrace();
        LogLine("socket-trace configuration enabled installed=%d", socketTraceInstalled ? 1 : 0);
    } else {
        LogLine("socket-trace disabled (90CN_socket_trace.ini missing or trace.enabled=0)");
    }
    bool contractUseInstalled = InstallContractUseCompatibility();
    LogLine("contract-use compatibility result=%d", contractUseInstalled ? 1 : 0);
    return 0;
}
#endif

bool LoadOptionalLuaPlugin()
{
    wchar_t enabled[8] = {};
    DWORD enabledLength = GetEnvironmentVariableW(
        L"DNF_LUA_PLUGIN", enabled, static_cast<DWORD>(_countof(enabled)));
    if (enabledLength > 0 && enabled[0] == L'0') {
        LogLine("optional Lua plugin disabled by DNF_LUA_PLUGIN=0");
        return false;
    }

    HMODULE selfModule = nullptr;
    if (!GetModuleHandleExW(
            GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS |
                GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT,
            reinterpret_cast<LPCWSTR>(&LoadOptionalLuaPlugin), &selfModule)) {
        LogLine("optional Lua plugin could not resolve 90CN module error=%u",
            GetLastError());
        return false;
    }

    wchar_t pluginPath[32768] = {};
    DWORD pathLength = GetModuleFileNameW(
        selfModule, pluginPath, static_cast<DWORD>(_countof(pluginPath)));
    if (pathLength == 0 || pathLength >= _countof(pluginPath)) {
        LogLine("optional Lua plugin could not resolve module path error=%u",
            GetLastError());
        return false;
    }
    wchar_t* separator = wcsrchr(pluginPath, L'\\');
    if (!separator) {
        LogLine("optional Lua plugin module path has no directory separator");
        return false;
    }
    separator[1] = L'\0';
    if (wcscat_s(pluginPath, L"90CNLua.dll") != 0) {
        LogLine("optional Lua plugin path is too long");
        return false;
    }

    DWORD attributes = GetFileAttributesW(pluginPath);
    if (attributes == INVALID_FILE_ATTRIBUTES ||
        (attributes & FILE_ATTRIBUTE_DIRECTORY) != 0) {
        if (enabledLength > 0 && enabled[0] == L'1') {
            LogLine("DNF_LUA_PLUGIN=1 but sibling 90CNLua.dll is missing");
        }
        return false;
    }

    HMODULE plugin = LoadLibraryW(pluginPath);
    if (!plugin) {
        LogLine("optional Lua plugin LoadLibraryW failed error=%u",
            GetLastError());
        return false;
    }
    using InstallLuaPlugin = BOOL(WINAPI*)();
    auto install = reinterpret_cast<InstallLuaPlugin>(
        GetProcAddress(plugin, "Install90CNLua"));
    if (!install || !install()) {
        LogLine("optional Lua plugin install export failed error=%u",
            GetLastError());
        return false;
    }
    LogLine("optional Lua plugin loaded module=%p", plugin);
    return true;
}

// Production boundary: connection bootstrap, packet codec, packet transport,
// bounded protocol routing, one validated native party-directory UI/refresh
// patch,
// and narrowly allow-listed compatibility for dungeon pickup, creature rename,
// premium-contract op44 dispatch, and server-owned Crystal/Aura state projection.
DWORD WINAPI InstallTransportWorkerInner(void*)
{
    g_dnfBase = reinterpret_cast<uintptr_t>(GetModuleHandleW(nullptr));
    if (!g_exceptionTraceHandle) {
        g_exceptionTraceHandle =
            AddVectoredExceptionHandler(1, TraceUnhandledClientException);
    }
    LogLine("90CN transport worker started exception_handler=%p module_base=0x%08X",
        g_exceptionTraceHandle, g_dnfBase);

    bool luaPluginLoaded = LoadOptionalLuaPlugin();
    LogLine("optional Lua plugin result=%d", luaPluginLoaded ? 1 : 0);

    wchar_t protocolTrace[8] = { 0 };
    DWORD protocolTraceLength =
        GetEnvironmentVariableW(L"DNF_PROTOCOL_TRACE", protocolTrace, 8);
    InterlockedExchange(
        &g_protocolTraceEnabled,
        protocolTraceLength > 0 && protocolTrace[0] == L'1' ? 1 : 0);
    LogLine("protocol transport trace enabled=%d",
        InterlockedCompareExchange(&g_protocolTraceEnabled, 0, 0) != 0 ? 1 : 0);

    g_checksumFn = g_dnfBase + kChecksumRva;
    g_gameAllocatorFn = g_dnfBase + kGameAllocatorRva;
    g_gameMemcpyFn = g_dnfBase + kGameMemcpyRva;
    g_dprotoOutgoingResume = g_dnfBase + kDprotoOutgoingResumeRva;
    g_dprotoOutgoingReturn = g_dnfBase + kDprotoOutgoingReturnRva;
    g_dprotoFalseResume = g_dnfBase + kDprotoFalseResumeRva;

    // The launcher opts in with DNF_MULTI_CLIENT=1. Keep this limited to the
    // two audited current-EXE single-instance checks; normal launches are no-op.
    bool multiClientInstalled = InstallMultiClientCompatibilityHook();
    LogLine("multi-client startup compatibility result=%d",
        multiClientInstalled ? 1 : 0);

    bool tclsInstalled = InstallTclsCompatibility();
    LogLine("TCLS transport bootstrap result=%d", tclsInstalled ? 1 : 0);

    // The channel hooks only observe directory loading and adapt the bootstrap
    // connection port. They do not write HUD, clock, scene, or actor state.
    bool channelInstalled = InstallChannelDiagnostic();
    LogLine("channel transport compatibility result=%d",
        channelInstalled ? 1 : 0);

    bool partyDirectoryUiInstalled =
        InstallPartyDirectoryFullPageCompatibility();
    LogLine("party-directory full-page UI compatibility result=%d",
        partyDirectoryUiInstalled ? 1 : 0);
    bool townCopresenceInstalled =
        InstallTownCopresenceCompatibility();
    LogLine("town co-presence actor-context compatibility result=%d",
        townCopresenceInstalled ? 1 : 0);
    bool dungeonPickupInstalled = InstallDungeonPickupCompatibility();
    LogLine("dungeon pickup compatibility result=%d",
        dungeonPickupInstalled ? 1 : 0);
    bool creatureRenameInstalled = InstallCreatureRenameCompatibility();
    LogLine("creature rename compatibility result=%d",
        creatureRenameInstalled ? 1 : 0);
    bool petEnchantDisplayInstalled =
        InstallPetEnchantDisplayCompatibility();
    LogLine("pet enchant display compatibility result=%d",
        petEnchantDisplayInstalled ? 1 : 0);

    wchar_t enabled[8] = { 0 };
    DWORD enabledLength =
        GetEnvironmentVariableW(L"DNF_CIPHER_PASSTHROUGH", enabled, 8);
    if (enabledLength > 0 && enabled[0] == L'0') {
        LogLine("DNF_CIPHER_PASSTHROUGH=0; packet codec hook disabled");
        return 0;
    }

    wchar_t dprotoEnabled[8] = { 0 };
    DWORD dprotoEnabledLength =
        GetEnvironmentVariableW(L"DNF_DPROTO_COMPAT", dprotoEnabled, 8);
    bool enableDprotoCompat =
        !(dprotoEnabledLength > 0 && dprotoEnabled[0] == L'0');

    wchar_t routeEnabled[8] = { 0 };
    DWORD routeEnabledLength =
        GetEnvironmentVariableW(L"DNF_ROUTE_COMPAT", routeEnabled, 8);
    bool enableSceneRouteCompat =
        routeEnabledLength == 0 || routeEnabled[0] != L'0';

    uintptr_t cipherTarget = g_dnfBase + kCipherEncodeRva;
    uintptr_t cipherDecodeTarget = g_dnfBase + kCipherDecodeRva;
    uintptr_t routePredicateTarget = g_dnfBase + kDprotoRoutePredicateRva;
    uintptr_t outgoingGateTarget = g_dnfBase + kDprotoOutgoingGateRva;
    uintptr_t outgoingCloneTarget = g_dnfBase + kDprotoOutgoingCloneRva;

    static const unsigned char kNativePrologue[] = {
        0x55, 0x8B, 0xEC, 0x53, 0x8B, 0x5D
    };
    static const unsigned char kRoutePredicatePrologue[] = {
        0x55, 0x8B, 0xEC, 0x8B, 0x49, 0x0C
    };
    static const unsigned char kOutgoingGate[] = {
        0x0F, 0x84, 0x2F, 0x01, 0x00, 0x00
    };
    static const unsigned char kOutgoingClonePrologue[] = {
        0x8B, 0x4B, 0x0C, 0x8B, 0x01
    };

    constexpr int kWaitLimitMs = 120000;
    int waitedMs = 0;
    while (waitedMs < kWaitLimitMs) {
        bool cipherReady = BytesMatch(
            reinterpret_cast<unsigned char*>(cipherTarget),
            kNativePrologue, sizeof(kNativePrologue));
        bool cipherDecodeReady = BytesMatch(
            reinterpret_cast<unsigned char*>(cipherDecodeTarget),
            kNativePrologue, sizeof(kNativePrologue));
        bool routeReady = !enableSceneRouteCompat ||
            BytesMatch(reinterpret_cast<unsigned char*>(routePredicateTarget),
                kRoutePredicatePrologue, sizeof(kRoutePredicatePrologue));
        bool dprotoReady = !enableDprotoCompat ||
            (BytesMatch(reinterpret_cast<unsigned char*>(routePredicateTarget),
                 kRoutePredicatePrologue, sizeof(kRoutePredicatePrologue)) &&
             BytesMatch(reinterpret_cast<unsigned char*>(outgoingGateTarget),
                 kOutgoingGate, sizeof(kOutgoingGate)) &&
             BytesMatch(reinterpret_cast<unsigned char*>(outgoingCloneTarget),
                 kOutgoingClonePrologue, sizeof(kOutgoingClonePrologue)));
        if (cipherReady && cipherDecodeReady && routeReady && dprotoReady) {
            break;
        }

        unsigned char firstByte = 0;
        if (TryReadByte(reinterpret_cast<unsigned char*>(cipherTarget),
                &firstByte) && firstByte == 0xE9) {
            LogLine("cipher encode already hooked; fresh DNF process required");
            return 0;
        }
        if (TryReadByte(reinterpret_cast<unsigned char*>(cipherDecodeTarget),
                &firstByte) && firstByte == 0xE9) {
            LogLine("cipher decode already hooked; fresh DNF process required");
            return 0;
        }
        Sleep(250);
        waitedMs += 250;
    }
    if (waitedMs >= kWaitLimitMs) {
        LogLine("transport targets unavailable route=%d dproto=%d",
            enableSceneRouteCompat ? 1 : 0,
            enableDprotoCompat ? 1 : 0);
        return 0;
    }

    unsigned char encodePatch[5] = { 0xE9 };
    intptr_t relative =
        reinterpret_cast<intptr_t>(&ProxyCipherEncodeFunction) -
        static_cast<intptr_t>(cipherTarget + 5);
    *reinterpret_cast<int32_t*>(encodePatch + 1) =
        static_cast<int32_t>(relative);
    if (!WriteCodePatch(cipherTarget, encodePatch, sizeof(encodePatch),
            "upper body encode transport")) {
        return 0;
    }

    unsigned char decodePatch[5] = { 0xE9 };
    relative = reinterpret_cast<intptr_t>(&ProxyCipherDecodeFunction) -
        static_cast<intptr_t>(cipherDecodeTarget + 5);
    *reinterpret_cast<int32_t*>(decodePatch + 1) =
        static_cast<int32_t>(relative);
    if (!WriteCodePatch(cipherDecodeTarget, decodePatch, sizeof(decodePatch),
            "upper body decode transport")) {
        return 0;
    }

    if (enableSceneRouteCompat) {
        unsigned char routePatch[6] = { 0xE9 };
        relative = reinterpret_cast<intptr_t>(&ProxySceneRouteAllowList) -
            static_cast<intptr_t>(routePredicateTarget + 5);
        *reinterpret_cast<int32_t*>(routePatch + 1) =
            static_cast<int32_t>(relative);
        routePatch[5] = 0x90;
        if (!WriteCodePatch(routePredicateTarget, routePatch,
                sizeof(routePatch), "inbound protocol route")) {
            return 0;
        }
    }

    if (enableDprotoCompat) {
        unsigned char clonePatch[5] = { 0xE9 };
        relative = reinterpret_cast<intptr_t>(&ProxyDprotoSendDirect) -
            static_cast<intptr_t>(outgoingCloneTarget + 5);
        *reinterpret_cast<int32_t*>(clonePatch + 1) =
            static_cast<int32_t>(relative);

        unsigned char falsePatch[6] = { 0x0F, 0x84 };
        relative = reinterpret_cast<intptr_t>(&ProxyDprotoFalseSelective) -
            static_cast<intptr_t>(outgoingGateTarget + 6);
        *reinterpret_cast<int32_t*>(falsePatch + 2) =
            static_cast<int32_t>(relative);

        if (!WriteCodePatch(outgoingCloneTarget, clonePatch,
                sizeof(clonePatch), "outbound DPROTO direct transport") ||
            !WriteCodePatch(outgoingGateTarget, falsePatch,
                sizeof(falsePatch), "outbound DPROTO selective transport")) {
            LogLine("DPROTO transport patch incomplete; fresh DNF process required");
            return 0;
        }
    }

    bool premiumStateInstalled = InstallPremiumStateCompatibility();
    LogLine("premium-state compatibility result=%d",
        premiumStateInstalled ? 1 : 0);
    bool contractUseInstalled = InstallContractUseCompatibility();
    LogLine("contract-use compatibility result=%d",
        contractUseInstalled ? 1 : 0);

    LogLine("transport DLL installed codec=1 decode=1 route=%d dproto=%d party_ui=%d town_copresence=%d dungeon_pickup=%d creature_rename=%d premium_state=%d contract_use=%d wait_ms=%d",
        enableSceneRouteCompat ? 1 : 0,
        enableDprotoCompat ? 1 : 0,
        partyDirectoryUiInstalled ? 1 : 0,
        townCopresenceInstalled ? 1 : 0,
        dungeonPickupInstalled ? 1 : 0,
        creatureRenameInstalled ? 1 : 0,
        premiumStateInstalled ? 1 : 0,
        contractUseInstalled ? 1 : 0,
        waitedMs);
    return 0;
}

DWORD WINAPI InstallWorker(void*)
{
    __try {
        return InstallTransportWorkerInner(nullptr);
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("worker exception code=0x%08X", GetExceptionCode());
        return 0;
    }
}
} // namespace

#pragma comment(linker, "/EXPORT:Queue90CNClientNotice=_Queue90CNClientNotice@8")
#pragma comment(linker, "/EXPORT:Dequeue90CNClientEvent=_Dequeue90CNClientEvent@8")
#pragma comment(linker, "/EXPORT:Query90CNCharacterStatSnapshot=_Query90CNCharacterStatSnapshot@8")
#pragma comment(linker, "/EXPORT:Query90CNEquipmentSnapshot=_Query90CNEquipmentSnapshot@8")
#pragma comment(linker, "/EXPORT:Query90CNDamageAffixSnapshot=_Query90CNDamageAffixSnapshot@8")
#pragma comment(linker, "/EXPORT:Update90CNCombatPanel=_Update90CNCombatPanel@8")

EXTERN_C BOOL WINAPI Queue90CNClientNotice(
    const wchar_t* text, unsigned int length)
{
    return QueueLuaClientNoticeInternal(text, length) ? TRUE : FALSE;
}

EXTERN_C BOOL WINAPI Dequeue90CNClientEvent(
    DNF90ClientEvent* output, unsigned int outputSize)
{
    if (!output || outputSize < DNF90_CLIENT_EVENT_V1_SIZE) return FALSE;

    DNF90ClientEvent event = {};
    if (!DequeueLuaClientEventInternal(&event)) return FALSE;
    const unsigned int copySize = outputSize >= sizeof(event)
        ? static_cast<unsigned int>(sizeof(event))
        : DNF90_CLIENT_EVENT_V1_SIZE;
    event.size = copySize;
    __try {
        memcpy(output, &event, copySize);
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("lua client event output write exception code=0x%08X",
            GetExceptionCode());
        return FALSE;
    }
    return TRUE;
}

EXTERN_C BOOL WINAPI Query90CNCharacterStatSnapshot(
    DNF90CharacterStatSnapshot* output, unsigned int outputSize)
{
    if (!output || outputSize < sizeof(DNF90CharacterStatSnapshot)) {
        return FALSE;
    }

    DNF90CharacterStatSnapshot snapshot = {};
    AcquireSRWLockShared(&g_characterStatSnapshotLock);
    snapshot = g_characterStatSnapshot;
    ReleaseSRWLockShared(&g_characterStatSnapshotLock);
    if (snapshot.size != sizeof(snapshot) || snapshot.generation == 0 ||
        (snapshot.validFlags & DNF90_CHARACTER_STATS_BASE_VALID) == 0) {
        return FALSE;
    }

    __try {
        *output = snapshot;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("combat-power snapshot output write exception code=0x%08X",
            GetExceptionCode());
        return FALSE;
    }
    return TRUE;
}

EXTERN_C BOOL WINAPI Query90CNEquipmentSnapshot(
    DNF90EquipmentSnapshot* output, unsigned int outputSize)
{
    if (!output || outputSize < sizeof(DNF90EquipmentSnapshot)) {
        return FALSE;
    }

    DNF90EquipmentSnapshot snapshot = {};
    AcquireSRWLockShared(&g_equipmentSnapshotLock);
    snapshot = g_equipmentSnapshot;
    ReleaseSRWLockShared(&g_equipmentSnapshotLock);
    if (snapshot.size != sizeof(snapshot) || snapshot.generation == 0 ||
        snapshot.itemCount > DNF90_EQUIPMENT_SNAPSHOT_MAX_ITEMS ||
        (snapshot.validFlags & DNF90_EQUIPMENT_SNAPSHOT_ROWS_VALID) == 0) {
        return FALSE;
    }

    __try {
        *output = snapshot;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("combat-power equipment snapshot output write exception "
            "code=0x%08X", GetExceptionCode());
        return FALSE;
    }
    return TRUE;
}

EXTERN_C BOOL WINAPI Query90CNDamageAffixSnapshot(
    DNF90DamageAffixSnapshot* output, unsigned int outputSize)
{
    if (!output || outputSize < sizeof(DNF90DamageAffixSnapshot)) {
        return FALSE;
    }

    DNF90DamageAffixSnapshot snapshot = {};
    AcquireSRWLockShared(&g_damageAffixSnapshotLock);
    snapshot = g_damageAffixSnapshot;
    ReleaseSRWLockShared(&g_damageAffixSnapshotLock);
    if (snapshot.size != sizeof(snapshot) || snapshot.generation == 0 ||
        snapshot.version != kCombatPowerAffixPrivateVersion ||
        snapshot.equippedItemCount > DNF90_EQUIPMENT_SNAPSHOT_MAX_ITEMS ||
        (snapshot.validFlags &
            (DNF90_DAMAGE_AFFIX_SNAPSHOT_VALUES_VALID |
             DNF90_DAMAGE_AFFIX_SNAPSHOT_IDENTITY_VALID |
             DNF90_DAMAGE_AFFIX_SNAPSHOT_THREE_ATTACKS_VALID |
             DNF90_DAMAGE_AFFIX_SNAPSHOT_EQUIPMENT_SCORE_VALID)) !=
            (DNF90_DAMAGE_AFFIX_SNAPSHOT_VALUES_VALID |
             DNF90_DAMAGE_AFFIX_SNAPSHOT_IDENTITY_VALID |
             DNF90_DAMAGE_AFFIX_SNAPSHOT_THREE_ATTACKS_VALID |
             DNF90_DAMAGE_AFFIX_SNAPSHOT_EQUIPMENT_SCORE_VALID)) {
        return FALSE;
    }

    __try {
        *output = snapshot;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("combat-power damage-affix snapshot output write exception code=0x%08X",
            GetExceptionCode());
        return FALSE;
    }
    return TRUE;
}

EXTERN_C BOOL WINAPI Update90CNCombatPanel(
    const DNF90CombatPanelState* state, unsigned int stateSize)
{
    if (!state || stateSize != sizeof(DNF90CombatPanelState)) return FALSE;

    DNF90CombatPanelState pending = {};
    __try {
        pending = *state;
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        LogLine("combat-panel state input read exception code=0x%08X",
            GetExceptionCode());
        return FALSE;
    }
    constexpr unsigned int kKnownFlags =
        DNF90_COMBAT_PANEL_ENABLED |
        DNF90_COMBAT_PANEL_BASE_SCORE_VALID |
        DNF90_COMBAT_PANEL_EQUIPMENT_SCORE_VALID |
        DNF90_COMBAT_PANEL_DAMAGE_AFFIXES_VALID |
        DNF90_COMBAT_PANEL_IDENTITY_VALID |
        DNF90_COMBAT_PANEL_THREE_ATTACKS_VALID;
    pending.professionUtf8[_countof(pending.professionUtf8) - 1] = '\0';
    const bool identityValid =
        (pending.validFlags & DNF90_COMBAT_PANEL_IDENTITY_VALID) != 0;
    if (pending.size != sizeof(pending) ||
        (pending.validFlags & ~kKnownFlags) != 0 ||
        pending.formulaVersion == 0 ||
        pending.totalScore > 2000000000u ||
        pending.baseAttributeScore > 2000000000u ||
        pending.equipmentScore > 2000000000u ||
        pending.whiteDamageTenths > 65535u ||
        pending.yellowDamageTenths > 65535u ||
        pending.criticalDamageTenths > 65535u ||
        pending.yellowAdditionalTenths > 65535u ||
        pending.criticalAdditionalTenths > 65535u ||
        pending.allAttackTenths > 65535u ||
        pending.equippedItemCount > DNF90_EQUIPMENT_SNAPSHOT_MAX_ITEMS ||
        (identityValid && (pending.level == 0 ||
            pending.professionUtf8[0] == '\0' ||
            MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS,
                pending.professionUtf8, -1, nullptr, 0) <= 0))) {
        return FALSE;
    }

    AcquireSRWLockExclusive(&g_combatPanelStateLock);
    g_combatPanelState = pending;
    ReleaseSRWLockExclusive(&g_combatPanelStateLock);

    HWND bridgeWindow = reinterpret_cast<HWND>(
        InterlockedCompareExchangePointer(
            &g_luaNoticeBridgeWindow, nullptr, nullptr));
    UINT bridgeMessage = static_cast<UINT>(
        InterlockedCompareExchange(&g_combatPanelWindowMessage, 0, 0));
    if (!bridgeWindow || !bridgeMessage ||
        !PostMessageW(bridgeWindow, bridgeMessage, 0, 0)) {
        return FALSE;
    }
    return TRUE;
}

void Start90CNPatch()
{
    // Do not chain-load the previous client hook.  This DLL owns only the
    // common codec and bounded current-scene route predicate boundary. It does
    // not hook send/recv or replay packet bodies.
    HANDLE worker = CreateThread(nullptr, 0, InstallWorker, nullptr, 0, nullptr);
    if (worker) CloseHandle(worker);
    else LogLine("CreateThread failed: %u", GetLastError());
}
