// Code generated from NoPack.exe runtime GetPacketName table; DO NOT EDIT.
// 本表只用于服务端日志命名和后续分发对齐，不能直接替代已验证的 GameType 行为。
package dnfenum

// CmdPacket 是 NoPack.exe runtime GetPacketName 导出的命令名表。
type CmdPacket uint16

const (
	CmdPacketCheckConnection                       CmdPacket = 0
	CmdPacketLogin                                 CmdPacket = 1
	CmdPacketSetUDPIPPort                          CmdPacket = 2
	CmdPacketExit                                  CmdPacket = 3
	CmdPacketSelectCharacter                       CmdPacket = 4
	CmdPacketCreateCharacter                       CmdPacket = 5
	CmdPacketDeleteCharacter                       CmdPacket = 6
	CmdPacketReturnSelectCharacter                 CmdPacket = 7
	CmdPacketGetUserinfo                           CmdPacket = 8
	CmdPacketRecoverStamina                        CmdPacket = 9
	CmdPacketRequestPeer                           CmdPacket = 10
	CmdPacketResponsePeer                          CmdPacket = 11
	CmdPacketSetPartyInfo                          CmdPacket = 12
	CmdPacketLeaveParty                            CmdPacket = 13
	CmdPacketWalkoutPartyMember                    CmdPacket = 14
	CmdPacketEnterSelectDungeon                    CmdPacket = 15
	CmdPacketSelectDungeon                         CmdPacket = 16
	CmdPacketSendMessage                           CmdPacket = 17
	CmdPacketDeleteItem                            CmdPacket = 18
	CmdPacketMoveItemspace                         CmdPacket = 19
	CmdPacketSortItem                              CmdPacket = 20
	CmdPacketBuyItem                               CmdPacket = 21
	CmdPacketSellItem                              CmdPacket = 22
	CmdPacketRepairEquipment                       CmdPacket = 23
	CmdPacketSetItemtradeState                     CmdPacket = 24
	CmdPacketCompoundItem                          CmdPacket = 25
	CmdPacketDisjointItem                          CmdPacket = 26
	CmdPacketUseLotteryItem                        CmdPacket = 27
	CmdPacketChangeSkillslot                       CmdPacket = 28
	CmdPacketBuySkill                              CmdPacket = 29
	CmdPacketIncreaseStatus                        CmdPacket = 30
	CmdPacketAcceptQuest                           CmdPacket = 31
	CmdPacketGiveupQuest                           CmdPacket = 32
	CmdPacketSetQuestTrigger                       CmdPacket = 33
	CmdPacketFinishQuest                           CmdPacket = 34
	CmdPacketSetUserPosition                       CmdPacket = 35
	CmdPacketSetUserArea                           CmdPacket = 36
	CmdPacketFinishLoading                         CmdPacket = 37
	CmdPacketUseSkill                              CmdPacket = 38
	CmdPacketDieMonster                            CmdPacket = 39
	CmdPacketDieCharacter                          CmdPacket = 40
	CmdPacketUseCoin                               CmdPacket = 41
	CmdPacketGiveupGame                            CmdPacket = 42
	CmdPacketGetItem                               CmdPacket = 43
	CmdPacketUseStackable                          CmdPacket = 44
	CmdPacketMoveMap                               CmdPacket = 45
	CmdPacketSetPlayResult                         CmdPacket = 46
	CmdPacketDropItem                              CmdPacket = 47
	CmdPacketDecreaseDurability                    CmdPacket = 48
	CmdPacketReportBadP2pUser                      CmdPacket = 49
	CmdPacketMakePVPRoom                           CmdPacket = 50
	CmdPacketEnterPVPRoom                          CmdPacket = 51
	CmdPacketSetPVPSeatState                       CmdPacket = 52
	CmdPacketSetPVPReadyState                      CmdPacket = 53
	CmdPacketSetPVPTeamMode                        CmdPacket = 54
	CmdPacketDiePVPCharacter                       CmdPacket = 55
	CmdPacketPVPTimeOut                            CmdPacket = 56
	CmdPacketEndPVPResult                          CmdPacket = 57
	CmdPacketResPVPRank                            CmdPacket = 58
	CmdPacketSetPVPMapIndex                        CmdPacket = 59
	CmdPacketAddFriend                             CmdPacket = 60
	CmdPacketRemoveFriend                          CmdPacket = 61
	CmdPacketDebugCommand                          CmdPacket = 62
	CmdPacketCera                                  CmdPacket = 63
	CmdPacketBuyCerashopItem                       CmdPacket = 64
	CmdPacketGenCeraticket                         CmdPacket = 65
	CmdPacketRequestPvpexpOfWeek                   CmdPacket = 66
	CmdPacketGuildMemerList                        CmdPacket = 67
	CmdPacketCallGuildCreateRight                  CmdPacket = 68
	CmdPacketScoreScrollState                      CmdPacket = 69
	CmdPacketCardSelectRightState                  CmdPacket = 70
	CmdPacketSelectCard                            CmdPacket = 71
	CmdPacketEplpCommand                           CmdPacket = 72
	CmdPacketCallGuildLevelUp                      CmdPacket = 73
	CmdPacketGuildInfo                             CmdPacket = 74
	CmdPacketRequestGuildEnter                     CmdPacket = 75
	CmdPacketRequestMemberEnter                    CmdPacket = 76
	CmdPacketMemberEnterReply                      CmdPacket = 77
	CmdPacketMemberSecede                          CmdPacket = 78
	CmdPacketCallMemerList                         CmdPacket = 79
	CmdPacketUpgradeItem                           CmdPacket = 80
	CmdPacketResetItemAttr                         CmdPacket = 81
	CmdPacketBuyPrivateStoreItem                   CmdPacket = 82
	CmdPacketEnterPrivateStore                     CmdPacket = 83
	CmdPacketExitPrivateStore                      CmdPacket = 84
	CmdPacketCreatePrivateStore                    CmdPacket = 85
	CmdPacketRemovePrivateStore                    CmdPacket = 86
	CmdPacketCompleteDisplay                       CmdPacket = 87
	CmdPacketMoveToGate                            CmdPacket = 88
	CmdPacketMakeWarroomTemp                       CmdPacket = 89
	CmdPacketEnterWarroom                          CmdPacket = 90
	CmdPacketSetWarroomSeatState                   CmdPacket = 91
	CmdPacketDieWarroomCharacter                   CmdPacket = 92
	CmdPacketStartWarroomTemp                      CmdPacket = 93
	CmdPacketMailboxSend                           CmdPacket = 94
	CmdPacketMailboxExtractItem                    CmdPacket = 95
	CmdPacketMailboxOpen                           CmdPacket = 96
	CmdPacketPeerConnectResult                     CmdPacket = 97
	CmdPacketQuickJoinRoom                         CmdPacket = 98
	CmdPacketCompoundAvatar                        CmdPacket = 99
	CmdPacketRenameCreature                        CmdPacket = 100
	CmdPacketResponseCreature                      CmdPacket = 101
	CmdPacketHatchCreature                         CmdPacket = 102
	CmdPacketBuyAutomatItem                        CmdPacket = 103
	CmdPacketRequestAvagachaCoupon                 CmdPacket = 104
	CmdPacketGatheringPartyStatus                  CmdPacket = 105
	CmdPacketWorldCupHitCount                      CmdPacket = 106
	CmdPacketGMCommand                             CmdPacket = 107
	CmdPacketReport4Hack                           CmdPacket = 108
	CmdPacketGuildWarInfo                          CmdPacket = 109
	CmdPacketPVPHeartBeat                          CmdPacket = 110
	CmdPacketCodeChecksum                          CmdPacket = 111
	CmdPacketPVPRequestFight                       CmdPacket = 112
	CmdPacketMouseregister                         CmdPacket = 113
	CmdPacketCreatureSendMessage                   CmdPacket = 114
	CmdPacketTraceError                            CmdPacket = 115
	CmdPacketOtherUserInfo                         CmdPacket = 116
	CmdPacketBossDieCheck                          CmdPacket = 117
	CmdPacketRegisiterToBlacklist                  CmdPacket = 118
	CmdPacketDeleteToBlacklist                     CmdPacket = 119
	CmdPacketRequestBlacklist                      CmdPacket = 120
	CmdPacketChangeHost                            CmdPacket = 121
	CmdPacketCreatureScriptMessage                 CmdPacket = 122
	CmdPacketCharacterStatistic                    CmdPacket = 123
	CmdPacketReportClientSpec                      CmdPacket = 124
	CmdPacketGuildmemberNaming                     CmdPacket = 125
	CmdPacketSetSubGuildMaster                     CmdPacket = 126
	CmdPacketExchangeServerInfo                    CmdPacket = 127
	CmdPacketExchangeServerInfoRet                 CmdPacket = 128
	CmdPacketExchangeServerCharacInfo              CmdPacket = 129
	CmdPacketExchangeServerCharacInfoRet           CmdPacket = 130
	CmdPacketTimeCheck                             CmdPacket = 131
	CmdPacketBack2Village                          CmdPacket = 132
	CmdPacketDnfRadioListen                        CmdPacket = 133
	CmdPacketChangeLetterStat                      CmdPacket = 134
	CmdPacketChangeCharacName                      CmdPacket = 135
	CmdPacketQueryCharacInfo                       CmdPacket = 136
	CmdPacketReportMannerlessUser                  CmdPacket = 137
	CmdPacketAlldieMonster                         CmdPacket = 138
	CmdPacketGuildMemerListNext                    CmdPacket = 139
	CmdPacketGuildAllMemberList                    CmdPacket = 140
	CmdPacketGuildAllMemberListNext                CmdPacket = 141
	CmdPacketRpyHumanCertify                       CmdPacket = 142
	CmdPacketChangeTutorialFlag                    CmdPacket = 143
	CmdPacketDieAiCharacter                        CmdPacket = 144
	CmdPacketCompleteLoadAssault                   CmdPacket = 145
	CmdPacketConnectP2pAssault                     CmdPacket = 146
	CmdPacketDieAssaultPlayer                      CmdPacket = 147
	CmdPacketRevivalAssaultPlayer                  CmdPacket = 148
	CmdPacketChangeHp                              CmdPacket = 149
	CmdPacketBvhackinfo                            CmdPacket = 150
	CmdPacketCallGuildInvite                       CmdPacket = 151
	CmdPacketReplyGuildInvite                      CmdPacket = 152
	CmdPacketReqGuildSecede                        CmdPacket = 153
	CmdPacketNotifyMessageToGuild                  CmdPacket = 154
	CmdPacketGuildMasterDelegate                   CmdPacket = 155
	CmdPacketCheckGuildNameDouble                  CmdPacket = 156
	CmdPacketCheckGuildAddreassDouble              CmdPacket = 157
	CmdPacketOpenGuildCreateWindow                 CmdPacket = 158
	CmdPacketDeathTowerStageCmd                    CmdPacket = 159
	CmdPacketUseBoosterItem                        CmdPacket = 160
	CmdPacketSecurityCardIssue                     CmdPacket = 161
	CmdPacketSecurityCardDisuse                    CmdPacket = 162
	CmdPacketSecurityCardAuthReq                   CmdPacket = 163
	CmdPacketSecurityCardAuthRpy                   CmdPacket = 164
	CmdPacketSecurityCardCertKey                   CmdPacket = 165
	CmdPacketCallPartyMemberRealtimeInfo           CmdPacket = 166
	CmdPacketEvadeAssault                          CmdPacket = 167
	CmdPacketAgreeEnchant                          CmdPacket = 168
	CmdPacketTryEnchant                            CmdPacket = 169
	CmdPacketPutItemForEnchant                     CmdPacket = 170
	CmdPacketClientSpecStatistic                   CmdPacket = 171
	CmdPacketSecurityCardAuthCancel                CmdPacket = 172
	CmdPacketHatchCreatureEgg                      CmdPacket = 173
	CmdPacketRequestHatchedCreature                CmdPacket = 174
	CmdPacketRequestCreatureCoupon                 CmdPacket = 175
	CmdPacketGmdebugCommand                        CmdPacket = 176
	CmdPacketJoinPower                             CmdPacket = 177
	CmdPacketSecedePower                           CmdPacket = 178
	CmdPacketChangeGuildName                       CmdPacket = 179
	CmdPacketSdcDamageCheck                        CmdPacket = 180
	CmdPacketSdcActivestatusCheck                  CmdPacket = 181
	CmdPacketAuctionAskAveragePrice                CmdPacket = 182
	CmdPacketAuctionRegistItem                     CmdPacket = 183
	CmdPacketAuctionRegistCancel                   CmdPacket = 184
	CmdPacketAuctionBidding                        CmdPacket = 185
	CmdPacketAuctionSearchByItemkey                CmdPacket = 186
	CmdPacketAuctionSearchByNoitemkey              CmdPacket = 187
	CmdPacketAuctionMyRegistedItemInfo             CmdPacket = 188
	CmdPacketAuctionMyBiddingInfo                  CmdPacket = 189
	CmdPacketAuctionMyAuctionHistory               CmdPacket = 190
	CmdPacketDungeonEventStoryPause                CmdPacket = 191
	CmdPacketJoinPowerWar                          CmdPacket = 192
	CmdPacketGoblinPadStatus                       CmdPacket = 193
	CmdPacketFrameLagStatistics                    CmdPacket = 194
	CmdPacketPVPChannelInfo                        CmdPacket = 195
	CmdPacketRequestMatch                          CmdPacket = 196
	CmdPacketSaveGameOption1                       CmdPacket = 197
	CmdPacketSaveGameOption2                       CmdPacket = 198
	CmdPacketSecurityCardRetransfer                CmdPacket = 199
	CmdPacketCeraIdentify                          CmdPacket = 200
	CmdPacketUseEmblem                             CmdPacket = 201
	CmdPacketDisjointAvatar                        CmdPacket = 202
	CmdPacketBeijingOlympicHitCount                CmdPacket = 203
	CmdPacketPurifyItem                            CmdPacket = 204
	CmdPacketInvestItemAmplifyOption               CmdPacket = 205
	CmdPacketAddAvatarSocket                       CmdPacket = 206
	CmdPacketShopCoinEvent                         CmdPacket = 207
	CmdPacketUseRandomboxItem                      CmdPacket = 208
	CmdPacketUDPCharacteristic                     CmdPacket = 209
	CmdPacketOnedayLethe                           CmdPacket = 210
	CmdPacketDisguiseRequest                       CmdPacket = 211
	CmdPacketDisguiseCancel                        CmdPacket = 212
	CmdPacketRequestPCRoomPlayerList               CmdPacket = 213
	CmdPacketRequestPCRoomPlayerCount              CmdPacket = 214
	CmdPacketUseVendingMachine                     CmdPacket = 215
	CmdPacketAssertInfo                            CmdPacket = 216
	CmdPacketOverflowInfo                          CmdPacket = 217
	CmdPacketServerMessageSend                     CmdPacket = 218
	CmdPacketServerMessageCheck                    CmdPacket = 219
	CmdPacketTestHackshieldRequest                 CmdPacket = 220
	CmdPacketHackshieldClientResponse              CmdPacket = 221
	CmdPacketGiveGiftToNPC                         CmdPacket = 222
	CmdPacketGoblinPadResponseCryptKey             CmdPacket = 223
	CmdPacketWriteGuildMemberMemo                  CmdPacket = 224
	CmdPacketSetPVPGold                            CmdPacket = 225
	CmdPacketCompoundCreature                      CmdPacket = 226
	CmdPacketCheckCreateGuildAgit                  CmdPacket = 227
	CmdPacketCreateGuildAgit                       CmdPacket = 228
	CmdPacketDeleteGuildAgit                       CmdPacket = 229
	CmdPacketPowerWarInfo                          CmdPacket = 230
	CmdPacketUpgradeGuildAgit                      CmdPacket = 231
	CmdPacketPowerWarProcessInfo                   CmdPacket = 232
	CmdPacketHackshieldMessageboxBug               CmdPacket = 233
	CmdPacketCreateDisjointStore                   CmdPacket = 234
	CmdPacketRequestDisjointItem                   CmdPacket = 235
	CmdPacketRepairDisjointMachine                 CmdPacket = 236
	CmdPacketTeleport                              CmdPacket = 237
	CmdPacketCompoundItemByExpertJob               CmdPacket = 238
	CmdPacketGiveupExpertJob                       CmdPacket = 239
	CmdPacketUpgradeDisjointMachine                CmdPacket = 240
	CmdPacketEnterDisjointStore                    CmdPacket = 241
	CmdPacketCloseDisjointStore                    CmdPacket = 242
	CmdPacketReportAbuseUser                       CmdPacket = 243
	CmdPacketLoadCompleteAfterAssault              CmdPacket = 244
	CmdPacketConnectP2pAfterAssault                CmdPacket = 245
	CmdPacketChangeNPCFavorDebug                   CmdPacket = 246
	CmdPacketGuildCargoPushItem                    CmdPacket = 247
	CmdPacketGuildCargoPopItem                     CmdPacket = 248
	CmdPacketGuildCargoMoveItem                    CmdPacket = 249
	CmdPacketLodingTimeReport                      CmdPacket = 250
	CmdPacketUseSharedEffectItem                   CmdPacket = 251
	CmdPacketBuyCerashopLimitItem                  CmdPacket = 252
	CmdPacketAddHacktypeCnt                        CmdPacket = 253
	CmdPacketChangeEmotion                         CmdPacket = 254
	CmdPacketDieBloodMonster                       CmdPacket = 255
	CmdPacketCompoundEmblem                        CmdPacket = 256
	CmdPacketMotionResult                          CmdPacket = 257
	CmdPacketBloodRoundUIPrepareFinish             CmdPacket = 258
	CmdPacketRequestConditionEventReward           CmdPacket = 259
	CmdPacketChangeAnotherSkillTree                CmdPacket = 260
	CmdPacketGuildCargo                            CmdPacket = 261
	CmdPacketGuildCargoHistory                     CmdPacket = 262
	CmdPacketFightVillageMonster                   CmdPacket = 263
	CmdPacketFinishVillageMonsterFighting          CmdPacket = 264
	CmdPacketUpgradeGuildCargo                     CmdPacket = 265
	CmdPacketMoveMapReport                         CmdPacket = 266
	CmdPacketRequestItemLock                       CmdPacket = 267
	CmdPacketRequestItemUnlock                     CmdPacket = 268
	CmdPacketRequestItemUnlockCancel               CmdPacket = 269
	CmdPacketRequestItemUnlockOtp                  CmdPacket = 270
	CmdPacketUpgradeChronicle                      CmdPacket = 271
	CmdPacketEnchantByBead                         CmdPacket = 272
	CmdPacketDungeonNPCBuffInfo                    CmdPacket = 273
	CmdPacketCreateWarroom                         CmdPacket = 274
	CmdPacketStartWarroom                          CmdPacket = 275
	CmdPacketWarroomChangeTeam                     CmdPacket = 276
	CmdPacketWarroomReady                          CmdPacket = 277
	CmdPacketWarroomRandomTeam                     CmdPacket = 278
	CmdPacketLagStatistics                         CmdPacket = 279
	CmdPacketHtPs                                  CmdPacket = 280
	CmdPacketHtIs                                  CmdPacket = 281
	CmdPacketHackLevelUp                           CmdPacket = 282
	CmdPacketPi                                    CmdPacket = 283
	CmdPacketVerifyGold                            CmdPacket = 284
	CmdPacketOntimeEventRequestReward              CmdPacket = 285
	CmdPacketRequestAddPVPBuddy                    CmdPacket = 286
	CmdPacketResponseAddPVPBuddy                   CmdPacket = 287
	CmdPacketRemovePVPBuddy                        CmdPacket = 288
	CmdPacketPVPBuddyConnList                      CmdPacket = 289
	CmdPacketAddUnitedServerFriend                 CmdPacket = 290
	CmdPacketDeleteUnitedServerFriend              CmdPacket = 291
	CmdPacketCheckFinishLoading                    CmdPacket = 292
	CmdPacketNcc                                   CmdPacket = 293
	CmdPacketMi                                    CmdPacket = 294
	CmdPacketChangeCharacSlot                      CmdPacket = 295
	CmdPacketSecretShopBuyItem                     CmdPacket = 296
	CmdPacketSecretShopOpenClose                   CmdPacket = 297
	CmdPacketCompleteLoadPVP                       CmdPacket = 298
	CmdPacketConnectP2pPVP                         CmdPacket = 299
	CmdPacketBiddingRoutingItem                    CmdPacket = 300
	CmdPacketUseSamsungRandomboxItem               CmdPacket = 301
	CmdPacketUseGoblinRandomboxItem                CmdPacket = 302
	CmdPacketBreakGuild                            CmdPacket = 303
	CmdPacketRequestQuestAutoClear                 CmdPacket = 304
	CmdPacketCreateAccountCargo                    CmdPacket = 305
	CmdPacketUpgradeAccountCargo                   CmdPacket = 306
	CmdPacketDepositMoney                          CmdPacket = 307
	CmdPacketWithdrawMoney                         CmdPacket = 308
	CmdPacketRedeemList                            CmdPacket = 309
	CmdPacketRedeem                                CmdPacket = 310
	CmdPacketSecuDataControl                       CmdPacket = 311
	CmdPacketConnectLinkCharac                     CmdPacket = 312
	CmdPacketDisconnectLinkCharac                  CmdPacket = 313
	CmdPacketChangeCharacLinkType                  CmdPacket = 314
	CmdPacketMultiMailboxSend                      CmdPacket = 315
	CmdPacketOperateRidableObject                  CmdPacket = 316
	CmdPacketSelectUltimateDifficulty              CmdPacket = 317
	CmdPacketPiv                                   CmdPacket = 318
	CmdPacketPid                                   CmdPacket = 319
	CmdPacketEcoEventItem                          CmdPacket = 320
	CmdPacketEnterVipRoom                          CmdPacket = 321
	CmdPacketGetDetectiveGoblinItem                CmdPacket = 322
	CmdPacketUseCreatureEvolutionItem              CmdPacket = 323
	CmdPacketQueryCharacInfoMailbox                CmdPacket = 324
	CmdPacketCompoundItemBindShpere                CmdPacket = 325
	CmdPacketUseRidable                            CmdPacket = 326
	CmdPacketCancelRidable                         CmdPacket = 327
	CmdPacketChangePartyPosition                   CmdPacket = 328
	CmdPacketOneToOneChatState                     CmdPacket = 329
	CmdPacketFindCharNameUseCharacNo               CmdPacket = 330
	CmdPacketSkillCommandCustomizing               CmdPacket = 331
	CmdPacketSkillCommandAllDefault                CmdPacket = 332
	CmdPacketHackScriptHash                        CmdPacket = 333
	CmdPacketAuctionBuyItemApiece                  CmdPacket = 334
	CmdPacketChangePartyMemberPosition             CmdPacket = 335
	CmdPacketAccff                                 CmdPacket = 336
	CmdPacketScanBotByDll                          CmdPacket = 337
	CmdPacketUseLimitCube                          CmdPacket = 338
	CmdPacketRefreshGuildInfo                      CmdPacket = 339
	CmdPacketOpenGuildBoard                        CmdPacket = 340
	CmdPacketWriteOnTheGuildboard                  CmdPacket = 341
	CmdPacketDeleteGuildboardText                  CmdPacket = 342
	CmdPacketRefreshGuildboard                     CmdPacket = 343
	CmdPacketRequestVideoObserver                  CmdPacket = 344
	CmdPacketStopVideoObserver                     CmdPacket = 345
	CmdPacketDonateGuildFund                       CmdPacket = 346
	CmdPacketCheckJoinGuild                        CmdPacket = 347
	CmdPacketRequestJoinGuild                      CmdPacket = 348
	CmdPacketCancelJoinGuild                       CmdPacket = 349
	CmdPacketApproveJoinGuild                      CmdPacket = 350
	CmdPacketDenyJoinGuild                         CmdPacket = 351
	CmdPacketGuildJoinList                         CmdPacket = 352
	CmdPacketRequestVideoObserverError             CmdPacket = 353
	CmdPacketResponseVideoObserver                 CmdPacket = 354
	CmdPacketGuildAttendanceInfo                   CmdPacket = 355
	CmdPacketPassGateObject                        CmdPacket = 356
	CmdPacketDungeonMotion                         CmdPacket = 357
	CmdPacketRequestOverseer                       CmdPacket = 358
	CmdPacketInsertOverseer                        CmdPacket = 359
	CmdPacketCompoundEquipmentUpgradeCard          CmdPacket = 360
	CmdPacketRefundCeraItem                        CmdPacket = 361
	CmdPacketPickupCeraItem                        CmdPacket = 362
	CmdPacketCashInventory                         CmdPacket = 363
	CmdPacketBreakAwayQuestCheck                   CmdPacket = 364
	CmdPacketJoinGuildInfo                         CmdPacket = 365
	CmdPacketScanBotByDrv                          CmdPacket = 366
	CmdPacketAskRematch                            CmdPacket = 367
	CmdPacketSaveGameOptionQuickchatting           CmdPacket = 368
	CmdPacketSelect3rdChronicleItemForEnchant      CmdPacket = 369
	CmdPacketEnchant3rdChronicleItem               CmdPacket = 370
	CmdPacketGoldTakeIncreasingAmount              CmdPacket = 371
	CmdPacketUseHackByOtherPartyMemberUid          CmdPacket = 372
	CmdPacketCheckSecurityProtection               CmdPacket = 373
	CmdPacketIntegrateMatchPVPScore                CmdPacket = 374
	CmdPacketFairPVPScore                          CmdPacket = 375
	CmdPacketPVPMissionHpPercent                   CmdPacket = 376
	CmdPacketPVPMissionWinPose                     CmdPacket = 377
	CmdPacketWarroomWpPerMonster                   CmdPacket = 378
	CmdPacketSetLabyrinthReadyState                CmdPacket = 379
	CmdPacketSetLabyrinthSeatState                 CmdPacket = 380
	CmdPacketRequestLabyrinthMonsterUid            CmdPacket = 381
	CmdPacketFinishLoadingLabyrinth                CmdPacket = 382
	CmdPacketDieLabyrinthMonster                   CmdPacket = 383
	CmdPacketDestroyLabyrinthObject                CmdPacket = 384
	CmdPacketDieLabyrinthCharacter                 CmdPacket = 385
	CmdPacketDestroyLabyrinthCenterObject          CmdPacket = 386
	CmdPacketRequestDungeonPartyList               CmdPacket = 387
	CmdPacketRegisterCargoPad                      CmdPacket = 388
	CmdPacketModifyCargoPad                        CmdPacket = 389
	CmdPacketUnregisterCargoPad                    CmdPacket = 390
	CmdPacketCancelCargoPad                        CmdPacket = 391
	CmdPacketCargoPadStatus                        CmdPacket = 392
	CmdPacketCargopadKeyReq                        CmdPacket = 393
	CmdPacketCargopadCertify                       CmdPacket = 394
	CmdPacketRequestVideoObserverLog               CmdPacket = 395
	CmdPacketAbnormalFunctionCall                  CmdPacket = 396
	CmdPacketEquipslotSwitch                       CmdPacket = 397
	CmdPacketExpandEquipslotFlagUpdate             CmdPacket = 398
	CmdPacketBuyCharacStatusUsingQp                CmdPacket = 399
	CmdPacketClearUsedQp                           CmdPacket = 400
	CmdPacketUnsealRandomOption                    CmdPacket = 401
	CmdPacketMobileReqAuthNo                       CmdPacket = 402
	CmdPacketMobileReqVerifyAuthNo                 CmdPacket = 403
	CmdPacketChangeHostWarroom                     CmdPacket = 404
	CmdPacketVerifyPrivateStoreItem                CmdPacket = 405
	CmdPacketSelectItem                            CmdPacket = 406
	CmdPacketRegenerationRandomOption              CmdPacket = 407
	CmdPacketUpgradeCargo                          CmdPacket = 408
	CmdPacketSelectIdolPot                         CmdPacket = 409
	CmdPacketIdolBringUp                           CmdPacket = 410
	CmdPacketPVPMissionCombo                       CmdPacket = 411
	CmdPacketTitleBookPut                          CmdPacket = 412
	CmdPacketTitleBookGet                          CmdPacket = 413
	CmdPacketMonstercardBind                       CmdPacket = 414
	CmdPacketCharacSlotExtendEffect                CmdPacket = 415
	CmdPacketExpertExtraction                      CmdPacket = 416
	CmdPacketAchievementTrigger                    CmdPacket = 417
	CmdPacketRequestEventServerLevelUp             CmdPacket = 418
	CmdPacketInviteMemberForGroup                  CmdPacket = 419
	CmdPacketLeaveFromGroup                        CmdPacket = 420
	CmdPacketChangeGroupChatUserState              CmdPacket = 421
	CmdPacketEventAllUserGift                      CmdPacket = 422
	CmdPacketOtherUserTitleBookList                CmdPacket = 423
	CmdPacketItemHyperlinkMessage                  CmdPacket = 424
	CmdPacketUserHistoryLog                        CmdPacket = 425
	CmdPacketRefundSkill                           CmdPacket = 426
	CmdPacketRegisterPlayer                        CmdPacket = 427
	CmdPacketAttendanceCheck                       CmdPacket = 428
	CmdPacketRequestGoblinPadImg                   CmdPacket = 429
	CmdPacketUpgradeInventory                      CmdPacket = 430
	CmdPacketSelectItemGrowthPower                 CmdPacket = 431
	CmdPacketRequestSeriaBuff                      CmdPacket = 432
	CmdPacketUseChestnutStoneItem                  CmdPacket = 433
	CmdPacketPartyTeleport                         CmdPacket = 434
	CmdPacketPartyTeleportConfirm                  CmdPacket = 435
	CmdPacketAbnormalUseStackable                  CmdPacket = 436
	CmdPacketChangeRandomOption                    CmdPacket = 437
	CmdPacketUpgradeItemSeparate                   CmdPacket = 438
	CmdPacketItemDictionary                        CmdPacket = 439
	CmdPacketMercenaryReturn                       CmdPacket = 440
	CmdPacketMercenaryInfo                         CmdPacket = 441
	CmdPacketMercenaryCompetition                  CmdPacket = 442
	CmdPacketRegisterQuickParty                    CmdPacket = 443
	CmdPacketCancelQuickParty                      CmdPacket = 444
	CmdPacketDirectEntranceDungeonQuickParty       CmdPacket = 445
	CmdPacketRequestAssaultPrice                   CmdPacket = 446
	CmdPacketSaveCharacterOption                   CmdPacket = 447
	CmdPacketExchangeRandomItemReward              CmdPacket = 448
	CmdPacketAvatarDisjointRandomReward            CmdPacket = 449
	CmdPacketCheck3rdpartyConcent                  CmdPacket = 450
	CmdPacketLoggingCryptedType                    CmdPacket = 451
	CmdPacketFloatRdataModulation                  CmdPacket = 452
	CmdPacketReqUrgentQuest                        CmdPacket = 453
	CmdPacketInsertRandomOptionItem                CmdPacket = 454
	CmdPacketResetRandomOption                     CmdPacket = 455
	CmdPacketClearQuest                            CmdPacket = 456
	CmdPacketTournamentRewardSelectState           CmdPacket = 457
	CmdPacketTournamentRewardSelect                CmdPacket = 458
	CmdPacketAvatarOptionChange                    CmdPacket = 459
	CmdPacketCharacterStatus                       CmdPacket = 460
	CmdPacketRequestSocialEventCoinItem            CmdPacket = 461
	CmdPacketRequestSocialEventMember              CmdPacket = 462
	CmdPacketResponseSocialEventMember             CmdPacket = 463
	CmdPacketLimitNPCBuyItem                       CmdPacket = 464
	CmdPacketQueryCharacInfoAddData                CmdPacket = 465
	CmdPacket3MonthStopStatistic                   CmdPacket = 466
	CmdPacketSeriaRoomDecoInfo                     CmdPacket = 467
	CmdPacketObjectBringUpUseItem                  CmdPacket = 468
	CmdPacketPrecheckSoloTelepoart                 CmdPacket = 469
	CmdPacketSoloTelepoart                         CmdPacket = 470
	CmdPacket2012NeweventPutitem                   CmdPacket = 471
	CmdPacketSaveGameOptionChattingEmoticon        CmdPacket = 472
	CmdPacketReportClientHack                      CmdPacket = 473
	CmdPacketRdataSectionModulation                CmdPacket = 474
	CmdPacketImageCommunicationEquipmentUse        CmdPacket = 475
	CmdPacketCompatibilityIndex                    CmdPacket = 476
	CmdPacketInformNotice                          CmdPacket = 477
	CmdPacketP2pStatistics                         CmdPacket = 478
	CmdPacketVerifyCreatureQuest                   CmdPacket = 479
	CmdPacketReportPVPLag                          CmdPacket = 480
	CmdPacketVerifyPVPLagUser                      CmdPacket = 481
	CmdPacketCollectItems                          CmdPacket = 482
	CmdPacketTutorialLevelUp                       CmdPacket = 483
	CmdPacketRequestCharacSkillInfo                CmdPacket = 484
	CmdPacketCraneStartUse                         CmdPacket = 485
	CmdPacketCranePickup                           CmdPacket = 486
	CmdPacketSelectStriker                         CmdPacket = 487
	CmdPacketRequestIngameAdvertisement            CmdPacket = 488
	CmdPacketLogIngameAdvertisement                CmdPacket = 489
	CmdPacketAutoSkill                             CmdPacket = 490
	CmdPacketSkillInit                             CmdPacket = 491
	CmdPacketPCRoomPlayTimeReward                  CmdPacket = 492
	CmdPacketPCRoomRentItem                        CmdPacket = 493
	CmdPacketSeriaroomDecoEvent                    CmdPacket = 494
	CmdPacketBlueMarble                            CmdPacket = 495
	CmdPacketGetGrowthcapsule                      CmdPacket = 496
	CmdPacketDynamicScriptReloading                CmdPacket = 497
	CmdPacketUseDye                                CmdPacket = 498
	CmdPacketPVPHistoryLog                         CmdPacket = 499
	CmdPacketPVPUseSkill                           CmdPacket = 500
	CmdPacketOnTimeEventWhileOneHourGift           CmdPacket = 501
	CmdPacketUseRightOfChangeGrowType              CmdPacket = 502
	CmdPacketInformNotice2nd                       CmdPacket = 503
	CmdPacketGrowthWeaponChangeInfinity            CmdPacket = 504
	CmdPacketGrowthWeaponUseMaterial               CmdPacket = 505
	CmdPacketSaveQuestNotify                       CmdPacket = 506
	CmdPacketBlueMarbleConfirmInfo                 CmdPacket = 507
	CmdPacketComboSkillInfo                        CmdPacket = 508
	CmdPacketUseRenameCard                         CmdPacket = 509
	CmdPacketComboSkillExtensionQuickSlotReset     CmdPacket = 510
	CmdPacketEquipedCreatureChangeInfinityCreature CmdPacket = 511
	CmdPacketSeriaroomAnimationDecoEvent           CmdPacket = 512
	CmdPacketBingoReward                           CmdPacket = 513
	CmdPacketBingoQuiz                             CmdPacket = 514
	CmdPacketUseStackableAction                    CmdPacket = 515
	CmdPacketDualRaidDungeonJoin                   CmdPacket = 516
	CmdPacketDualRaidDungeon                       CmdPacket = 517
	CmdPacketOpenCerapackage                       CmdPacket = 518
	CmdPacketGetItembox                            CmdPacket = 519
	CmdPacketRequestIntegratedMatching             CmdPacket = 520
	CmdPacketCancelIntegratedMatching              CmdPacket = 521
	CmdPacketMatchingDungeonExit                   CmdPacket = 522
	CmdPacketChannelMoveSuccess                    CmdPacket = 523
	CmdPacketRequestEventRanking                   CmdPacket = 524
	CmdPacketAddEventRanking                       CmdPacket = 525
	CmdPacketRequestColosseumPurchaseTicket        CmdPacket = 526
	CmdPacketRequestUpdateColosseumReward          CmdPacket = 527
	CmdPacketSummonMonster                         CmdPacket = 528
	CmdPacketRacingDungeonJoin                     CmdPacket = 529
	CmdPacketRacingDungeonDisjoin                  CmdPacket = 530
	CmdPacketRacingDungeonGoalInPlayer             CmdPacket = 531
	CmdPacketRacingDungeonReturnToVillage          CmdPacket = 532
	CmdPacketDualRaidDungeonReady                  CmdPacket = 533
	CmdPacketDualRaidDungeonReward                 CmdPacket = 534
	CmdPacketUpdateContractOfCubeInfo              CmdPacket = 535
	CmdPacketRequestFreelyGiveItemBox              CmdPacket = 536
	CmdPacketRequestCouponChange                   CmdPacket = 537
	CmdPacketUseFreeGiveItemCoupon                 CmdPacket = 538
	CmdPacketChangePeriodicToUnlimitItem           CmdPacket = 539
	CmdPacketInitFreeGiveItemEvent                 CmdPacket = 540
	CmdPacketSelectZombie                          CmdPacket = 541
	CmdPacketToBeZombie                            CmdPacket = 542
	CmdPacketZombieModeResultScore                 CmdPacket = 543
	CmdPacketDecideTimeStepAttendance              CmdPacket = 544
	CmdPacketRequestForParticipation               CmdPacket = 546
	CmdPacketMonsterMoveSystem                     CmdPacket = 547
	CmdPacketKiriCargoPurchase                     CmdPacket = 548
	CmdPacketKiriCargoGetBonus                     CmdPacket = 549
	CmdPacketPVPTournamentMatchList                CmdPacket = 550
	CmdPacketPVPTournamentRequest                  CmdPacket = 551
	CmdPacketLoadingCart                           CmdPacket = 552
	CmdPacketGoldenCommerceStart                   CmdPacket = 553
	CmdPacketGoldenCommerceReward                  CmdPacket = 554
	CmdPacketCreatureFillpoint                     CmdPacket = 555
	CmdPacketChurnGradeActionClear                 CmdPacket = 556
	CmdPacketCargoTransportItem                    CmdPacket = 557
	CmdPacketDungeonMissionStart                   CmdPacket = 558
	CmdPacketDungeonMissionUpdate                  CmdPacket = 559
	CmdPacketDungeonMissionCheckSuccess            CmdPacket = 560
	CmdPacketRequestInstantReinforce               CmdPacket = 561
	CmdPacketFatigueAccelerationStateChange        CmdPacket = 562
	CmdPacketMinorityDetailResult                  CmdPacket = 563
	CmdPacketMinorityRequestQuestion               CmdPacket = 564
	CmdPacketMinorityQuestionAnswer                CmdPacket = 565
	CmdPacketMinorityRequireReward                 CmdPacket = 566
	CmdPacketDecideLevelup                         CmdPacket = 567
	CmdPacketSetCloneTitle                         CmdPacket = 568
	CmdPacketSurveyContents                        CmdPacket = 569
	CmdPacketIntegrateMatchingDirectComplete       CmdPacket = 570
	CmdPacketIntegratedMatchingModeChange          CmdPacket = 571
	CmdPacketIntegrateMatchingUserCount            CmdPacket = 572
	CmdPacketCollectPrivateData                    CmdPacket = 573
	CmdPacketCollectPrivateSerialData              CmdPacket = 574
	CmdPacketCollectPrivateNoReplay                CmdPacket = 575
	CmdPacketCreateUpgradeRoom                     CmdPacket = 576
	CmdPacketDestroyUpgradeRoom                    CmdPacket = 577
	CmdPacketJoinSpectatorUpgradeRoom              CmdPacket = 578
	CmdPacketLeaveSpectatorUpgradeRoom             CmdPacket = 579
	CmdPacketUpgradeRoomGiveItemInfo               CmdPacket = 580
	CmdPacketModuleExist                           CmdPacket = 581
	CmdPacketModuleRequest                         CmdPacket = 582
	CmdPacketRequestComboScore                     CmdPacket = 583
	CmdPacketComboScoreInfo                        CmdPacket = 584
	CmdPacketAddRainbowPoint                       CmdPacket = 585
	CmdPacketRequestRainbowPoint                   CmdPacket = 586
	CmdPacketRequestRainbowPointReward             CmdPacket = 587
	CmdPacketUpgradeRoomPutUpItem                  CmdPacket = 588
	CmdPacketUpgradeRoomUpgradeStart               CmdPacket = 589
	CmdPacketUpgradeRoomUpgradeCancel              CmdPacket = 590
	CmdPacketUpgradeRoomMasterMessage              CmdPacket = 591
	CmdPacketCharmProlong                          CmdPacket = 592
	CmdPacketSecurityStatus                        CmdPacket = 593
	CmdPacketEventNPCDropItem                      CmdPacket = 594
	CmdPacketRequestPickEventInfo                  CmdPacket = 595
	CmdPacketLetsPickPresent                       CmdPacket = 596
	CmdPacketRequestAddPickChance                  CmdPacket = 597
	CmdPacketCreateExpertJobStore                  CmdPacket = 598
	CmdPacketEnterExpertJobStore                   CmdPacket = 599
	CmdPacketCloseExpertJobStore                   CmdPacket = 600
	CmdPacketUseEnchantStore                       CmdPacket = 601
	CmdPacketGetExpandExpGageReward                CmdPacket = 602
	CmdPacketUpgradeCard                           CmdPacket = 603
	CmdPacketUserRankCombo                         CmdPacket = 604
	CmdPacketUseObjectScaleEffectInVillage         CmdPacket = 605
	CmdPacketCancleObjectScaleEffectInVillage      CmdPacket = 606
	CmdPacketRequestChildrenDayEventReward         CmdPacket = 607
	CmdPacketRequestExpKeepingEventReward          CmdPacket = 608
	CmdPacketRequestSpTeam                         CmdPacket = 609
	CmdPacketStrongestPVPPrivateInfo               CmdPacket = 610
	CmdPacketRepairExpertJobStore                  CmdPacket = 611
	CmdPacketAppendageDestoryObject                CmdPacket = 612
	CmdPacketStrongestPVPMemberChannelinfo         CmdPacket = 613
	CmdPacketUpdateEventDungeonTopRank             CmdPacket = 614
	CmdPacketTeraPieceClearTimeRankRequest         CmdPacket = 615
	CmdPacketBroadcastPVPMakeItem                  CmdPacket = 616
	CmdPacketBroadcastPVPSelectedCharacter         CmdPacket = 617
	CmdPacketTimerModifyInfo                       CmdPacket = 618
	CmdPacketSummonTimeLine                        CmdPacket = 619
	CmdPacketSeaChaseMiniGameResult                CmdPacket = 620
	CmdPacketRequestUpdateSpecEvent                CmdPacket = 621
	CmdPacketRequestSpecEventReward                CmdPacket = 622
	CmdPacketGainEatObject                         CmdPacket = 623
	CmdPacketPVPDailyWinCount                      CmdPacket = 624
	CmdPacketDeletePVPFightDollTicket              CmdPacket = 625
	CmdPacketSelectFightDoll                       CmdPacket = 626
	CmdPacketUsePVPDollSkill                       CmdPacket = 627
	CmdPacketFightDollRankIdx                      CmdPacket = 628
	CmdPacketAttendanceMarbleEventInfo             CmdPacket = 629
	CmdPacketAttendanceMarbleEventDice             CmdPacket = 630
	CmdPacketUserStateMotion                       CmdPacket = 631
	CmdPacketGetPcroomTimePointItem                CmdPacket = 632
	CmdPacketGetAvatarSpecEvent                    CmdPacket = 633
	CmdPacketRequestEventDungeonTopRankInfo        CmdPacket = 634
	CmdPacketDungeonClear                          CmdPacket = 635
	CmdPacketDuringLoginTimeEventAction            CmdPacket = 636
	CmdPacketBidLegendaryAuction                   CmdPacket = 637
	CmdPacketCharacterCreateCountPerDayForKor      CmdPacket = 638
	CmdPacketReturnUserRewardRequest               CmdPacket = 639
	CmdPacketRequestFatigueUseReward               CmdPacket = 640
	CmdPacketAttendanceMarbleDoubleExit            CmdPacket = 641
	CmdPacketSetExpectPartyList                    CmdPacket = 642
	CmdPacketPVPRoomForSimulation                  CmdPacket = 643
	CmdPacketOtpCheck                              CmdPacket = 644
	CmdPacketCharacViewHiddenCharacInfo            CmdPacket = 645
	CmdPacketRebirthHardcoreCharac                 CmdPacket = 646
	CmdPacketHardcoreCharacList                    CmdPacket = 647
	CmdPacketHardcoreMurderer                      CmdPacket = 648
	CmdPacketRequestResetHardcoreCharac            CmdPacket = 649
	// CmdPacketHonorExpertState is the current NoPack class0/op649 meaning.
	// Keep the old generated name above as a compatibility alias only.
	CmdPacketHonorExpertState                        CmdPacket = CmdPacketRequestResetHardcoreCharac
	CmdPacketHardcoreRank                            CmdPacket = 650
	CmdPacketRequestEventGift                        CmdPacket = 651
	CmdPacketAssaultReviveUseMoney                   CmdPacket = 652
	CmdPacketDimensionExperienceUserReward           CmdPacket = 653
	CmdPacketDimensionExperienceMentor               CmdPacket = 654
	CmdPacketDimensionExperienceClearReward          CmdPacket = 655
	CmdPacketUsingSkillLog                           CmdPacket = 656
	CmdPacketChangeDeckInfo                          CmdPacket = 657
	CmdPacketRaidEntryCostInfo                       CmdPacket = 658
	CmdPacketRaidMovieSkip                           CmdPacket = 659
	CmdPacketSelectRaidRewardCard                    CmdPacket = 660
	CmdPacketRaidDoBehavior                          CmdPacket = 661
	CmdPacketRaidSetSymbol                           CmdPacket = 662
	CmdPacketRaidMessage                             CmdPacket = 663
	CmdPacketCreateRaid                              CmdPacket = 664
	CmdPacketLeaveRaid                               CmdPacket = 665
	CmdPacketStartRaid                               CmdPacket = 666
	CmdPacketSetRaidWaiting                          CmdPacket = 667
	CmdPacketRejoinRaid                              CmdPacket = 668
	CmdPacketRaidManagerWork                         CmdPacket = 669
	CmdPacketModifyRaidInfo                          CmdPacket = 670
	CmdPacketDisjointColosseumItem                   CmdPacket = 671
	CmdPacketRequestStarMarketingCreature            CmdPacket = 672
	CmdPacketChangeStarMarketingInfiniteCreature     CmdPacket = 673
	CmdPacketRecentFriendList                        CmdPacket = 674
	CmdPacketPVPSeasonReward                         CmdPacket = 675
	CmdPacketGunner2AwakeningUserReward              CmdPacket = 676
	CmdPacketGunner2AwakeningClearReward             CmdPacket = 677
	CmdPacketRequestItemGrowtypeExperience           CmdPacket = 678
	CmdPacketLoadExtendCharacs                       CmdPacket = 679
	CmdPacketStartSlotMachine                        CmdPacket = 680
	CmdPacketExecuteSlotMachine                      CmdPacket = 681
	CmdPacketBingoReset                              CmdPacket = 682
	CmdPacketReqHumanCertify                         CmdPacket = 683
	CmdPacketRecoveryDeleteCharacter                 CmdPacket = 684
	CmdPacketGetCouponSystemReward                   CmdPacket = 685
	CmdPacketLevelUpEventReward                      CmdPacket = 686
	CmdPacketIllusionUpgradeState                    CmdPacket = 687
	CmdPacketEventReward                             CmdPacket = 688
	CmdPacketEventRequest                            CmdPacket = 689
	CmdPacketStaticsRuntimeTing                      CmdPacket = 690
	CmdPacketReserveLeaveParty                       CmdPacket = 691
	CmdPacketCheckDoubleCharacterName                CmdPacket = 692
	CmdPacketCrackOfDimension                        CmdPacket = 693
	CmdPacketRaidBuffSystem                          CmdPacket = 694
	CmdPacketRaidMonsterHp                           CmdPacket = 695
	CmdPacketDungeonBonusMonster                     CmdPacket = 696
	CmdPacketPcroomAttendanceEventCheck              CmdPacket = 697
	CmdPacketUserAttendanceEventCheck                CmdPacket = 698
	CmdPacketDailyChallengeReward                    CmdPacket = 699
	CmdPacketRequestStudyJoinReward                  CmdPacket = 700
	CmdPacketFarmEventAction                         CmdPacket = 701
	CmdPacketUpdateSpecEvent                         CmdPacket = 702
	CmdPacketGetPcroomWithFriendItem                 CmdPacket = 703
	CmdPacketContentsPlayInfo                        CmdPacket = 704
	CmdPacketEntryIntoParty                          CmdPacket = 705
	CmdPacketEntryIntoPartyFinish                    CmdPacket = 706
	CmdPacketEquipmentSwapInfo                       CmdPacket = 707
	CmdPacketJoinRealEstate                          CmdPacket = 708
	CmdPacketLeaveRealEstate                         CmdPacket = 709
	CmdPacketStartRealEstate                         CmdPacket = 710
	CmdPacketRealEstateUserState                     CmdPacket = 711
	CmdPacketUDPPacketStatistic                      CmdPacket = 712
	CmdPacketRequestItemSortLock                     CmdPacket = 713
	CmdPacketRequestItemSortUnlock                   CmdPacket = 714
	CmdPacketShopPurchaseCount                       CmdPacket = 715
	CmdPacketSlotMachineUpdateItemList               CmdPacket = 716
	CmdPacketSetPcroomChoiceAndFocusMission          CmdPacket = 717
	CmdPacketCancelChoiceAndFocusMission             CmdPacket = 718
	CmdPacketGetPcroomChoiceAndFocusItem             CmdPacket = 719
	CmdPacketTerritoryCombatSymbol                   CmdPacket = 720
	CmdPacketTerritoryCombatRespawn                  CmdPacket = 721
	CmdPacketTerritoryCombatSituationRoom            CmdPacket = 722
	CmdPacketTerritoryCombatSkill                    CmdPacket = 723
	CmdPacketTerritoryCombatHp                       CmdPacket = 724
	CmdPacketTerritoryCombatSupplyItem               CmdPacket = 725
	CmdPacketTerritoryCombatRequestSupplyItem        CmdPacket = 726
	CmdPacketTerritoryCombatChangeRole               CmdPacket = 727
	CmdPacketTerritoryCombatSecretPath               CmdPacket = 728
	CmdPacketTerritoryCombatReqMoveDungeon           CmdPacket = 729
	CmdPacketTerritoryCombatResMoveDungeon           CmdPacket = 730
	CmdPacketTerritoryCombatWaitRoom                 CmdPacket = 731
	CmdPacketTerritoryCombatDungeonResult            CmdPacket = 732
	CmdPacketRentalItemEventInfo                     CmdPacket = 733
	CmdPacketDungeonDamageInfo                       CmdPacket = 734
	CmdPacketAnalysisDungeonDamageInfo               CmdPacket = 735
	CmdPacketUseGoldcardRealEstate                   CmdPacket = 736
	CmdPacketRejoinDungeon                           CmdPacket = 737
	CmdPacketCancelRejoinDungeon                     CmdPacket = 738
	CmdPacketCheckGuildCreatePromoteMsg              CmdPacket = 739
	CmdPacketModifyGuildPromoteMsg                   CmdPacket = 740
	CmdPacketRequestGuildCreatePermit                CmdPacket = 741
	CmdPacketReplyGuildCreatePermit                  CmdPacket = 742
	CmdPacketCancelGuildCreate                       CmdPacket = 743
	CmdPacketReqGuildInfoOfMyChars                   CmdPacket = 744
	CmdPacketReqGuildSerchForJoin                    CmdPacket = 745
	CmdPacketTodayGuildAttendanceDetailInfo          CmdPacket = 746
	CmdPacketReqGuildMileageHistory                  CmdPacket = 747
	CmdPacketChangeGuildGrade                        CmdPacket = 748
	CmdPacketChangeGuildGradeName                    CmdPacket = 749
	CmdPacketVariableNeedMaterial                    CmdPacket = 750
	CmdPacketTryPuzzle                               CmdPacket = 751
	CmdPacketFpsPerformanceStatistic                 CmdPacket = 752
	CmdPacketSetUserAreaPreCheck                     CmdPacket = 753
	CmdPacketReqGuildAlliance                        CmdPacket = 754
	CmdPacketReqGuildAllianceList                    CmdPacket = 755
	CmdPacketCancelReqGuildAlliance                  CmdPacket = 756
	CmdPacketCallGuildAllianceList                   CmdPacket = 757
	CmdPacketApproveGuildAlliance                    CmdPacket = 758
	CmdPacketDenyGuildAlliance                       CmdPacket = 759
	CmdPacketSecedeGuildAlliance                     CmdPacket = 760
	CmdPacketBuyGuildContents                        CmdPacket = 761
	CmdPacketReqRecommendGuild                       CmdPacket = 762
	CmdPacketSendGuildPresent                        CmdPacket = 763
	CmdPacketReqGuildPresentsList                    CmdPacket = 764
	CmdPacketReqGuildPresentsHistory                 CmdPacket = 765
	CmdPacketRecvGuildPresent                        CmdPacket = 766
	CmdPacketRegisterGuildSupportCharac              CmdPacket = 767
	CmdPacketRemoveGuildSupportCharac                CmdPacket = 768
	CmdPacketRentGuildSupportCharac                  CmdPacket = 769
	CmdPacketRentCancelGuildSupportCharac            CmdPacket = 770
	CmdPacketInfoGuildSupportCharac                  CmdPacket = 771
	CmdPacketUseGuildSupportCharac                   CmdPacket = 772
	CmdPacketWebViewerInfo                           CmdPacket = 773
	CmdPacketRequestSceneStreamReplay                CmdPacket = 774
	CmdPacketResponseSceneStreamReplay               CmdPacket = 775
	CmdPacketDestroyAssaultObject                    CmdPacket = 776
	CmdPacketRequestCircleEnter                      CmdPacket = 777
	CmdPacketCheckTerritoryCombatDeclaration         CmdPacket = 778
	CmdPacketReqTerritoryCombatDeclaration           CmdPacket = 779
	CmdPacketReqTerritoryCombatDeclarationList       CmdPacket = 780
	CmdPacketCheckTerritoryCombatIndirectPart        CmdPacket = 781
	CmdPacketReqTerritoryCombatIndirectPart          CmdPacket = 782
	CmdPacketReqTerritoryCombatIndirectPartStatus    CmdPacket = 783
	CmdPacketReqTerritoryCombatIndirectPartReward    CmdPacket = 784
	CmdPacketReGrowupChange                          CmdPacket = 785
	CmdPacketSkillQuickSlotSort                      CmdPacket = 786
	CmdPacketChangeGuildMark                         CmdPacket = 787
	CmdPacketEpicBookMakeItem                        CmdPacket = 788
	CmdPacketRequestServerCharacterList              CmdPacket = 789
	CmdPacketScenarioModeClearQuest                  CmdPacket = 790
	CmdPacketInputUserCongratulatoryTelegram         CmdPacket = 791
	CmdPacketAskForTerritoryCombat                   CmdPacket = 792
	CmdPacketReqTerritoryCombatList                  CmdPacket = 793
	CmdPacketDecideTerritoryCombatList               CmdPacket = 794
	CmdPacketReqDecideTerritoryCombatList            CmdPacket = 795
	CmdPacketCheckTerritoryCombatChannelEnter        CmdPacket = 796
	CmdPacketCheckTerritoryCombatExerciseModeTime    CmdPacket = 797
	CmdPacketAvatarContestInfo                       CmdPacket = 798
	CmdPacketAvatarContestSubmit                     CmdPacket = 799
	CmdPacketReqTerritoryOperationFundDistribution   CmdPacket = 800
	CmdPacketAvatarConversion                        CmdPacket = 801
	CmdPacketRequestObjectGrowth                     CmdPacket = 802
	CmdPacketBetFatigue                              CmdPacket = 803
	CmdPacketRequestUserChannel                      CmdPacket = 804
	CmdPacketRemakeJumpingCharacter                  CmdPacket = 805
	CmdPacketIncubatingSystemReward                  CmdPacket = 806
	CmdPacketIncubatingSystemSetGrowtype             CmdPacket = 807
	CmdPacketRequestWarroomReward                    CmdPacket = 808
	CmdPacketCommonStructSerializeSample             CmdPacket = 809
	CmdPacketGuildContributeHistory                  CmdPacket = 810
	CmdPacketReqRepresentCharacter                   CmdPacket = 811
	CmdPacketRequestNPCFavorOperation                CmdPacket = 812
	CmdPacketGiveNPCBuff                             CmdPacket = 813
	CmdPacketLicenseDungeonPlayResult                CmdPacket = 814
	CmdPacketLicenseDungeonRequestReward             CmdPacket = 815
	CmdPacketEpicPieceExchangeChoice                 CmdPacket = 816
	CmdPacketEpicPieceExchangeMaterialChoice         CmdPacket = 817
	CmdPacketEpicPieceExchangeComplete               CmdPacket = 818
	CmdPacketRaidOtherChannelUserinfo                CmdPacket = 819
	CmdPacketRaidOtherChannelRequestJoin             CmdPacket = 820
	CmdPacketRaidOtherChannelResponseJoin            CmdPacket = 821
	CmdPacketRaidRecentFriendList                    CmdPacket = 822
	CmdPacketRaidMemberChangeState                   CmdPacket = 823
	CmdPacketRaidUserMoveChannelFail                 CmdPacket = 824
	CmdPacketReqTerritoryCombatAllianceList          CmdPacket = 825
	CmdPacketReqTerritoryCombatPointList             CmdPacket = 826
	CmdPacketSetTerritoryOperationFundDistributeType CmdPacket = 827
	CmdPacketMoveMapObject                           CmdPacket = 828
	CmdPacketUseGem                                  CmdPacket = 829
	CmdPacketSetGuildRecommandChannel                CmdPacket = 830
	CmdPacketRaidOtherChannelList                    CmdPacket = 831
	CmdPacketSetGuildFlagAura                        CmdPacket = 832
	CmdPacketLockDisplayOtp                          CmdPacket = 833
	CmdPacketUnlockDisplayOtp                        CmdPacket = 834
	CmdPacketCompoundGem                             CmdPacket = 835
	CmdPacketCollectUserinfoBinary                   CmdPacket = 836
	CmdPacketAgitWarConstructBuilding                CmdPacket = 837
	CmdPacketAgitWarDestructBuilding                 CmdPacket = 838
	CmdPacketAgitWarMoveBuilding                     CmdPacket = 839
	CmdPacketAgitWarUpgradeBuilding                  CmdPacket = 840
	CmdPacketAgitWarMatching                         CmdPacket = 841
	CmdPacketAgitWarMatchingCancel                   CmdPacket = 842
	CmdPacketAgitwarWinpointReward                   CmdPacket = 843
	CmdPacketReplayZoneGiftCube                      CmdPacket = 844
	CmdPacketReplayZoneClickReplay                   CmdPacket = 845
	CmdPacketReplayZoneFavoriteUpdate                CmdPacket = 846
	CmdPacketReplayZoneFavoriteList                  CmdPacket = 847
	CmdPacketReplayZoneReqUserInfo                   CmdPacket = 848
	CmdPacketReplayZoneSelfDelete                    CmdPacket = 849
	CmdPacketReplayZoneReqBasicInfo                  CmdPacket = 850
	CmdPacketMainHudInfo                             CmdPacket = 851
	CmdPacketReqBingoMark                            CmdPacket = 852
	CmdPacketAccountAchievementTrigger               CmdPacket = 853
	CmdPacketSeasonServerUserInfo                    CmdPacket = 854
	CmdPacketRequestInfiniteDifficultyInfo           CmdPacket = 855
	CmdPacketRequestInfiniteDifficultyCharacInfo     CmdPacket = 856
	CmdPacketRequestInfiniteDifficultyRank           CmdPacket = 857
	CmdPacketAdventurerMakerCreate                   CmdPacket = 858
	CmdPacketAdventurerMakerGrowRequest              CmdPacket = 859
	CmdPacketAdventurerMakerInitialize               CmdPacket = 860
	CmdPacketAdventurerMakerGrowRest                 CmdPacket = 861
	CmdPacketAdventurerMakerAppearNPC                CmdPacket = 862
	CmdPacketOpenAuraSkinSlot                        CmdPacket = 863
	CmdPacketCompoundFlag                            CmdPacket = 864
	CmdPacketSeasonCharacInfo                        CmdPacket = 865
	CmdPacketSeasonCharacConvert                     CmdPacket = 866
	CmdPacketAccountAchievementReward                CmdPacket = 867
	CmdPacketSyncItemSpace                           CmdPacket = 868
	CmdPacketSequentialDungeonInfo                   CmdPacket = 869
	CmdPacketCheckPVPPrivateCharacter                CmdPacket = 870
	CmdPacketSetPVPTotalMatchTeam                    CmdPacket = 871
	CmdPacketCheckPVPTotalMatchTeamName              CmdPacket = 872
	CmdPacketSetPVPTotalMatchTeamName                CmdPacket = 873
	CmdPacketSetPVPTotalMatchCharacOrder             CmdPacket = 874
	CmdPacketSetAutoEquipment                        CmdPacket = 875
	CmdPacketPVPMissionFirstAerialAttack             CmdPacket = 876
	CmdPacketPVPMissionComboDamageRate               CmdPacket = 877
	CmdPacketPVPMissionReplayCount                   CmdPacket = 878
	CmdPacketPVPMissionArcadeMode                    CmdPacket = 879
	CmdPacketTransformationItem                      CmdPacket = 880
	CmdPacketSetPVPUserState                         CmdPacket = 881
	CmdPacketReqOpenHiddenQuest                      CmdPacket = 882
	CmdPacketReqSaveEquipInfo4bitanEvent             CmdPacket = 883
	CmdPacketActivateOddSocket                       CmdPacket = 884
	CmdPacketEnchantByOdd                            CmdPacket = 885
	CmdPacketCompoundOdd                             CmdPacket = 886
	CmdPacketExtractOdd                              CmdPacket = 887
	CmdPacketRaidRequestRaidMembers                  CmdPacket = 888
	CmdPacketRaidCheckRaidUser                       CmdPacket = 889
	CmdPacketSecondPasswordCheck                     CmdPacket = 890
	CmdPacketTradingSystem                           CmdPacket = 891
	CmdPacketPassRaidPhase                           CmdPacket = 892
	CmdPacketHousingTreeInfo                         CmdPacket = 893
	CmdPacketHousingSetNewTree                       CmdPacket = 894
	CmdPacketHousingGiveWater                        CmdPacket = 895
	CmdPacketHousingHarvestTree                      CmdPacket = 896
	CmdPacketHousingWaterHistory                     CmdPacket = 897
	CmdPacketHousingResetTree                        CmdPacket = 898
	CmdPacketBuyItemUsePoint                         CmdPacket = 899
	CmdPacketCheckShopEntrance                       CmdPacket = 900
	CmdPacketEventDnftrendGetReward                  CmdPacket = 901
	CmdPacketEventTenMinuteGetReward                 CmdPacket = 902
	CmdPacketPremiumService                          CmdPacket = 903
	CmdPacketAntibotDelayLog                         CmdPacket = 904
	CmdPacketEventDungeonDestoryObject               CmdPacket = 905
	CmdPacketEventDungeonClearRoom                   CmdPacket = 906
	CmdPacketChangeCreatureTradeAttr                 CmdPacket = 907
	CmdPacketLoggingClong                            CmdPacket = 908
	CmdPacketMapclearLogWarroom                      CmdPacket = 909
	CmdPacketStreetVictorBoardTopVictorData          CmdPacket = 910
	CmdPacketStreetVictorBoardMyData                 CmdPacket = 911
	CmdPacketInformLockTime                          CmdPacket = 912
	CmdPacketUseEmblemForEquipment                   CmdPacket = 913
	CmdPacketAddEquipmentSocket                      CmdPacket = 914
	CmdPacketConvertEmblem                           CmdPacket = 915
	CmdPacketReportBadUser                           CmdPacket = 916
	CmdPacketGetUserInfoForReportBadUser             CmdPacket = 917
	CmdPacketActionPointActionClear                  CmdPacket = 918
	CmdPacketActionPointGetRewardItem                CmdPacket = 919
	CmdPacketDecorationUseStackable                  CmdPacket = 920
	CmdPacketDecorationSetup                         CmdPacket = 921
	CmdPacketDecorationMoveInvenslot                 CmdPacket = 922
	CmdPacketHousingInviteFriend                     CmdPacket = 923
	CmdPacketHousingVisitRoom                        CmdPacket = 924
	CmdPacketHousingEnterRoom                        CmdPacket = 925
	CmdPacketHousingLeaveRoom                        CmdPacket = 926
	CmdPacketHousingSendMessage                      CmdPacket = 927
	CmdPacketPacketSwitchEquipslot                   CmdPacket = 928
	CmdPacketKillPlayerLog                           CmdPacket = 929
	CmdPacketJoinTournament                          CmdPacket = 930
	CmdPacketTournamentStatus                        CmdPacket = 931
	CmdPacketUseAvatarPottery                        CmdPacket = 932
	CmdPacketMoveItemToAccountCargo                  CmdPacket = 933
	CmdPacketAradAttendanceCheck                     CmdPacket = 934
	CmdPacketAradCompoundKatagaki                    CmdPacket = 935
	CmdPacketEventLoginAndGift                       CmdPacket = 936
	CmdPacketUsedGoldskill                           CmdPacket = 937
	CmdPacketAdvanceAltarStartGame                   CmdPacket = 938
	CmdPacketAdvanceAltarBuyItem                     CmdPacket = 939
	CmdPacketAdvanceAltarSetSlot                     CmdPacket = 940
	CmdPacketAdvanceAltarUpgradeGage                 CmdPacket = 941
	CmdPacketAdvanceAltarSummonUnit                  CmdPacket = 942
	CmdPacketAdvanceAltarExchangeSlot                CmdPacket = 943
	CmdPacketAdvanceAltarPause                       CmdPacket = 944
	CmdPacketAdvanceAltarGetAchievementReward        CmdPacket = 945
	CmdPacketAdvanceAltarResetStar                   CmdPacket = 946
	CmdPacketAdvanceAltarStageClearInfo              CmdPacket = 947
	CmdPacketAdvanceAltarFullGageReward              CmdPacket = 948
	CmdPacketReqIgaVer                               CmdPacket = 949
	CmdPacketPeriodItemRebuyStatistic                CmdPacket = 950
	CmdPacketAddEquipmentEffect                      CmdPacket = 951
	CmdPacketMinorityEvent                           CmdPacket = 952
	CmdPacketUseJumpingCharTicket                    CmdPacket = 953
	CmdPacketUseAvatarRoulette                       CmdPacket = 954
	CmdPacketAvatarCoinCount                         CmdPacket = 955
	CmdPacketAvatarHiddenOptionChange                CmdPacket = 956
	CmdPacketUseAvatarRechargeJpn                    CmdPacket = 957
	CmdPacketEmblemCompoundJpn                       CmdPacket = 958
	CmdPacketAvatarConvertJpn                        CmdPacket = 959
	CmdPacketReqAradConditionEventReward             CmdPacket = 960
	CmdPacketUseAncientMysticCube                    CmdPacket = 961
	CmdPacketQuizReqItem                             CmdPacket = 962
	CmdPacketQuizResItem                             CmdPacket = 963
	CmdPacketQuestBefore70LevelExpand                CmdPacket = 964
	CmdPacketLevelupSupportReqItem                   CmdPacket = 965
	CmdPacketP2pHolePunchingSuccessRate              CmdPacket = 966
	CmdPacketCharacterCreateCountPerDay              CmdPacket = 967
	CmdPacketUseTitleChangeItem                      CmdPacket = 968
	CmdPacketGetDanjinEventAvatarLimitBox            CmdPacket = 969
	CmdPacketGetDanjinEventAvatarUnlimitBox          CmdPacket = 970
	CmdPacketUseDanjinEventAvatarExtend              CmdPacket = 971
	CmdPacketDestoryDungeonStart                     CmdPacket = 972
	CmdPacketCompoundUniqueItem                      CmdPacket = 973
	CmdPacketDoubleAttendanceSetLogout               CmdPacket = 974
	CmdPacketReportPartyPlayManner                   CmdPacket = 975
	CmdPacketEmblemCollectItem                       CmdPacket = 976
	CmdPacketReadHuntingSkill                        CmdPacket = 977
	CmdPacketUpgradeCarryGold                        CmdPacket = 978
	CmdPacketApcTnInfo                               CmdPacket = 979
	CmdPacketApcTnEntrance                           CmdPacket = 980
	CmdPacketApcTnBetting                            CmdPacket = 981
	CmdPacketApcTnDividend                           CmdPacket = 982
	CmdPacketRequestPickedAvatar                     CmdPacket = 983
	CmdPacketSecretEventShopBuyItem                  CmdPacket = 984
	CmdPacketUserChoiceEvent                         CmdPacket = 985
	CmdPacketEventAttendanceReward                   CmdPacket = 986
	CmdPacketQqPcroomBenefitReward                   CmdPacket = 987
	CmdPacketEventCreateDnfRequest                   CmdPacket = 988
	CmdPacketRequestPcroomDayilyReward               CmdPacket = 989
	CmdPacketHeroMissionDataReward                   CmdPacket = 990
	CmdPacketRogerLevineAuctionBuynow                CmdPacket = 991
	CmdPacketRogerLevineAuctionBidding               CmdPacket = 992
	CmdPacketDungeonNPCShopBuyItem                   CmdPacket = 993
	CmdPacketDungeonNPCShopOpenClose                 CmdPacket = 994
	CmdPacketSeventhMissionReward                    CmdPacket = 995
	CmdPacketSeriaRidableInHiddenTruthDungeon        CmdPacket = 996
	CmdPacketDungeonClearReward                      CmdPacket = 997
	CmdPacketBuyMoonFestivalItem                     CmdPacket = 998
	CmdPacketRentEquipmentItem                       CmdPacket = 999
	CmdPacketChargeRentpoint                         CmdPacket = 1000
	CmdPacketRequestReceiveAttandanceReward          CmdPacket = 1001
	CmdPacketEventAccountFatigueStat                 CmdPacket = 1002
	CmdPacketGoldenEggRewardRequest                  CmdPacket = 1003
	CmdPacketLevelupSupport3rdEventGetItem           CmdPacket = 1004
	CmdPacketJuly7thRequestResponse                  CmdPacket = 1005
	CmdPacketDecideChallengeAttendance               CmdPacket = 1006
	CmdPacketTictactoeMarking                        CmdPacket = 1007
	CmdPacketTictactoeRequestData                    CmdPacket = 1008
	CmdPacketTictactoeReward                         CmdPacket = 1009
	CmdPacketExchangeItemFromNPC                     CmdPacket = 1010
	CmdPacketChainLettersReward                      CmdPacket = 1011
	CmdPacketSealCreature                            CmdPacket = 1012
	CmdPacketDnfWithFriendsRecommend                 CmdPacket = 1013
	CmdPacketDnfWithFriendsInfo                      CmdPacket = 1014
	CmdPacketDnfWithFriendsBuy                       CmdPacket = 1015
	CmdPacketHelpLevelUp                             CmdPacket = 1016
	CmdPacketEventGrowthEquipment                    CmdPacket = 1017
	CmdPacketShootingstarBuyItem                     CmdPacket = 1018
	CmdPacketDimensionPocketItem                     CmdPacket = 1019
	CmdPacketAccelerateGrowthSendGift                CmdPacket = 1020
	CmdPacketSelectCollectbox                        CmdPacket = 1021
	CmdPacketAddCollectboxItem                       CmdPacket = 1022
	CmdPacketRemoveCollectboxItem                    CmdPacket = 1023
	CmdPacketExtendCollectboxExpiryDate              CmdPacket = 1024
	CmdPacketTakeMagicLamp                           CmdPacket = 1025
	CmdPacketSendGlassBottleLetter                   CmdPacket = 1026
	CmdPacketFacebookLike                            CmdPacket = 1027
	CmdPacketOnTimeImdRequest                        CmdPacket = 1028
	CmdPacketTheSeaBottom2000Use                     CmdPacket = 1029
	CmdPacketUniqueBindShpereInfo                    CmdPacket = 1030
	CmdPacketPrepareNewBalance                       CmdPacket = 1031
	CmdPacketWeddingRequest                          CmdPacket = 1032
	CmdPacketWeddingResponse                         CmdPacket = 1033
	CmdPacketWeddingMoneyGift                        CmdPacket = 1034
	CmdPacketWeddingCharac                           CmdPacket = 1035
	CmdPacketWeddingUpgradeRing                      CmdPacket = 1036
	CmdPacketWeddingUseLovePointItem                 CmdPacket = 1037
	CmdPacketWeddingSendLetter                       CmdPacket = 1038
	CmdPacketWeddingEnterCeremony                    CmdPacket = 1039
	CmdPacketUpdateGuestComment                      CmdPacket = 1040
	CmdPacketDeleteGuestComment                      CmdPacket = 1041
	CmdPacketLoadGuestComment                        CmdPacket = 1042
	CmdPacketCheckMoveToPartner                      CmdPacket = 1043
	CmdPacketMoveToPartner                           CmdPacket = 1044
	CmdPacketCoupleInvenOpenClose                    CmdPacket = 1045
	CmdPacketCoupleRoomMoveItem                      CmdPacket = 1046
	CmdPacketCoupleInvenDeleteItem                   CmdPacket = 1047
	CmdPacketCoupleTreePlant                         CmdPacket = 1048
	CmdPacketCoupleTreeWater                         CmdPacket = 1049
	CmdPacketCoupleTreeHarvest                       CmdPacket = 1050
	CmdPacketCoupleTreeRemove                        CmdPacket = 1051
	CmdPacketMakeCoupleGrowthcapsule                 CmdPacket = 1052
	CmdPacketGiveCoupleGrowthcapsule                 CmdPacket = 1053
	CmdPacketTakeCoupleGrowthcapsule                 CmdPacket = 1054
	CmdPacketDragonDungeonTimeattackRankerBoard      CmdPacket = 1055
	CmdPacketBreakTrapResult                         CmdPacket = 1056
	CmdPacketEventChangeClass                        CmdPacket = 1057
	CmdPacketTripleExpKeepingReward                  CmdPacket = 1058
	CmdPacketSelectRandomFortune                     CmdPacket = 1059
	CmdPacketPlayBlueHorseSlot                       CmdPacket = 1060
	CmdPacketRewardBlueHorseSlot                     CmdPacket = 1061
	CmdPacketMysticAvatarAlpha                       CmdPacket = 1062
	CmdPacketYearEndLotto                            CmdPacket = 1063
	CmdPacketBingoStampInfo                          CmdPacket = 1064
	CmdPacketRetryBooterInfo                         CmdPacket = 1065
	CmdPacketOlympicMedalInfo                        CmdPacket = 1066
	CmdPacketVeryDifficultHellParty                  CmdPacket = 1067
	CmdPacketLevelDecision                           CmdPacket = 1068
	CmdPacketCompoundForDiametricallyItem            CmdPacket = 1069
	CmdPacketUniqueAdvancedBindShpere                CmdPacket = 1070
	CmdPacketEventQuestion                           CmdPacket = 1071
	CmdPacketDoblinFromStar                          CmdPacket = 1072
	CmdPacketMarbleDiceInfo                          CmdPacket = 1073
	CmdPacketMarbleDiceThrow                         CmdPacket = 1074
	CmdPacketMarbleDiceExit                          CmdPacket = 1075
	CmdPacketTeraPieceRank                           CmdPacket = 1076
	CmdPacketExchangeCeraPoint                       CmdPacket = 1077
	CmdPacketJobLevelDecisionSelect                  CmdPacket = 1078
	CmdPacketJobLevelDecisionInfo                    CmdPacket = 1079
	CmdPacketJobLevelDecisionChange                  CmdPacket = 1080
	CmdPacketPhoenixWeaponEventReward                CmdPacket = 1081
	CmdPacketPhoenixWeaponEventRewardMailCheckCharac CmdPacket = 1082
	CmdPacketPhoenixWeaponEventRewardMail            CmdPacket = 1083
	CmdPacketPhoenixWeaponEventChangePeriod          CmdPacket = 1084
	CmdPacketPhoenixWeaponEventChangeWeapon          CmdPacket = 1085
	CmdPacketFindSignsFinalRewardEvent               CmdPacket = 1086
	CmdPacketCelebrateBigTransitionEventReward       CmdPacket = 1087
	CmdPacket6thGoldcoin                             CmdPacket = 1088
	CmdPacketUpdateBestDamageRank                    CmdPacket = 1089
	CmdPacketRequestBestDamageRank                   CmdPacket = 1090
	CmdPacketSenseOfChoice                           CmdPacket = 1091
	CmdPacketDiceGame                                CmdPacket = 1092
	CmdPacketValentineEvent2014                      CmdPacket = 1093
	CmdPacket2014LieDayDelilahShopBuyJpn             CmdPacket = 1094
	CmdPacketEventSugartimeDayReward                 CmdPacket = 1095
	CmdPacketEventSugartimeFinalReward               CmdPacket = 1096
	CmdPacketEventSugartimeFinalPick                 CmdPacket = 1097
	CmdPacketDualRaidBossHpCheck                     CmdPacket = 1098
	CmdPacketCreatureExpBook                         CmdPacket = 1099
	CmdPacketUpgradeTicketSlotMachinePlay            CmdPacket = 1100
	CmdPacketUpgradeTicketSlotMachineOpen            CmdPacket = 1101
	CmdPacketPuzzleGameChoice                        CmdPacket = 1102
	CmdPacketPuzzleChanceReward                      CmdPacket = 1103
	CmdPacketTosCurDungeonInfo                       CmdPacket = 1104
	CmdPacketFatigueStepReward                       CmdPacket = 1105
	CmdPacketMissionRoulette                         CmdPacket = 1106
	CmdPacketMissionRouletteTrigger                  CmdPacket = 1107
	CmdPacketChangeAvatarClosetSet                   CmdPacket = 1108
	CmdPacketSelectAvatarCloset                      CmdPacket = 1109
	CmdPacketGunner2ndAttendanceRewardReq            CmdPacket = 1110
	CmdPacketChargingCharmEnergy                     CmdPacket = 1111
	CmdPacketEventQuickSlotReward                    CmdPacket = 1112
	CmdPacketEventTotalAttendanceCheckThisweek       CmdPacket = 1113
	CmdPacketLegendSwordInfo                         CmdPacket = 1114
	CmdPacketRequestRewardCompleteDecideLevelup      CmdPacket = 1115
	CmdPacketRequestRewardCompleteDecideState        CmdPacket = 1116
	CmdPacketUpgradeAvatarCloset                     CmdPacket = 1117
	CmdPacketHalloweenInfo                           CmdPacket = 1118
	CmdPacketBindPlus                                CmdPacket = 1119
	CmdPacketCardCompound                            CmdPacket = 1120
	CmdPacketDonateForEco                            CmdPacket = 1121
	CmdPacketEventItemRequest                        CmdPacket = 1122
	CmdPacketAttendanceCalendar                      CmdPacket = 1123
	CmdPacketXigncodeSecurityData                    CmdPacket = 1124
	CmdPacketChristmasPresentExchange                CmdPacket = 1125
	CmdPacketLongAttendanceChangeMission             CmdPacket = 1126
	CmdPacketProperDungeonClearCharacReward          CmdPacket = 1127
	CmdPacketUseRandomboxItemExpand                  CmdPacket = 1128
	CmdPacketBurningUpToSpecificLevel                CmdPacket = 1129
	CmdPacketPeerPingIndexUniv                       CmdPacket = 1130
	CmdPacketIncreaseChanceLotteryReset              CmdPacket = 1131
	CmdPacketGoalAttainment                          CmdPacket = 1132
	CmdPacketChronicleDisjoint                       CmdPacket = 1133
	CmdPacketMahjongStartgame                        CmdPacket = 1134
	CmdPacketMahjongGet                              CmdPacket = 1135
	CmdPacketMahjongDrop                             CmdPacket = 1136
	CmdPacketAimForTheLegendary                      CmdPacket = 1137
	CmdPacketPlayDnfAtLaborday                       CmdPacket = 1138
	CmdPacketProperDungeonCountAccount               CmdPacket = 1139
	CmdPacketFramelagPerDungeon                      CmdPacket = 1140
	CmdPacketPresentToPeris                          CmdPacket = 1141
	CmdPacketMachineCreatureChangeState              CmdPacket = 1142
	CmdPacketAccountOnceGiftEvent                    CmdPacket = 1143
	CmdPacketDungeonBossMapSelect                    CmdPacket = 1144
	CmdPacketWelcombackAttendance                    CmdPacket = 1145
	CmdPacketWelcombackDailyMission                  CmdPacket = 1146
	CmdPacketFoolsDaySyusiaPresent                   CmdPacket = 1147
	CmdPacketWelcombackDailyEquipReward              CmdPacket = 1148
	CmdPacketAddBuffVendingMachine                   CmdPacket = 1149
	CmdPacketKunoichiHotTraining                     CmdPacket = 1150
	CmdPacketNinjaSmithy                             CmdPacket = 1151
	CmdPacketGoldenBingoEvent                        CmdPacket = 1152
	CmdPacketEveryDayDfo                             CmdPacket = 1153
	CmdPacketSecurityCardEmailReqUniv                CmdPacket = 1154
	CmdPacketPartyTimeUniv                           CmdPacket = 1155
	CmdPacketSecurityCardCertKeyCancelUniv           CmdPacket = 1156
	CmdPacketPriestMissionReward                     CmdPacket = 1157
	CmdPacketPriestLevelupSupport                    CmdPacket = 1158
	CmdPacketPriestDimensionSupport                  CmdPacket = 1159
	CmdPacketLiaWalkieTalkieAttendence               CmdPacket = 1160
	CmdPacketMysticAvatar                            CmdPacket = 1161
	CmdPacketCommonStructSample                      CmdPacket = 1162
	CmdPacketSelectTimeAttendance                    CmdPacket = 1163
	CmdPacketGodGrowthSupport                        CmdPacket = 1164
	CmdPacketKaronLetheFree                          CmdPacket = 1165
	CmdPacketCommonBuffVendingMachineChn             CmdPacket = 1166
	CmdPacketAllUserGrowup                           CmdPacket = 1167
	CmdPacket7thAnniversary                          CmdPacket = 1168
	CmdPacketNewAccountRecommandFriend               CmdPacket = 1169
	CmdPacketNewAccountReqRecommandCountReward       CmdPacket = 1170
	CmdPacketSeriaClosetBuy                          CmdPacket = 1171
	CmdPacketSeriaClosetWear                         CmdPacket = 1172
	CmdPacketReqCircusDungeonTicket                  CmdPacket = 1173
	CmdPacketMakeUnderworldMapPiece                  CmdPacket = 1174
	CmdPacketReqUnderworldMapReward                  CmdPacket = 1175
	CmdPacketUsePayletterCoupon                      CmdPacket = 1176
	CmdPacket8weekAttendance                         CmdPacket = 1177
	CmdPacketAtswordmanPhoenixWeapon                 CmdPacket = 1178
	CmdPacketChangePhoenixWeapon                     CmdPacket = 1179
	CmdPacketUnlimitedPhoenixWeapon                  CmdPacket = 1180
	CmdPacketGetGameInfoGameOfDanjin                 CmdPacket = 1181
	CmdPacketRollDiceGameOfDanjin                    CmdPacket = 1182
	CmdPacketResetBoardGameOfDanjin                  CmdPacket = 1183
	CmdPacketForceBuyItemDiceGameOfDanjin            CmdPacket = 1184
	CmdPacketRenewalHotTimeEvent                     CmdPacket = 1185
	CmdPacketLudmillaSupport                         CmdPacket = 1186
	CmdPacketRequestRewardSecondAwakeningEventUniv   CmdPacket = 1187
	CmdPacketGetRewardInfoSecondAwakeningEventUniv   CmdPacket = 1188
	CmdPacketAboutHope                               CmdPacket = 1189
	CmdPacketMysteriousGrace                         CmdPacket = 1190
	CmdPacketLeaveInNationalday                      CmdPacket = 1191
	CmdPacketNationalDay2015                         CmdPacket = 1192
	CmdPacketEventPVPAccount                         CmdPacket = 1193
	CmdPacketClearComboWithProleague                 CmdPacket = 1194
	CmdPacketDailyCharacDungoneClearJpn              CmdPacket = 1195
	CmdPacketFighterSkillEventRewardJpn              CmdPacket = 1196
	CmdPacketFighterSkillEventUseSkillJpn            CmdPacket = 1197
	CmdPacketItemPickupEventJpn                      CmdPacket = 1198
	CmdPacketCrackOfDimmensionRewardJpn              CmdPacket = 1199
	CmdPacketHitHugePumpkinInfo                      CmdPacket = 1200
	CmdPacketHitHugePumpkinUseAx                     CmdPacket = 1201
	CmdPacketHotDeal                                 CmdPacket = 1202
	CmdPacketSoloday2ndLike                          CmdPacket = 1203
	CmdPacketGrowthWeaponRequest                     CmdPacket = 1204
	CmdPacketColosseumSeason3RequestGaraponJpn       CmdPacket = 1205
	CmdPacketNeosPremiumContractRentItem             CmdPacket = 1206
	CmdPacketNeosPremiumContractRequestGiftItem      CmdPacket = 1207
	CmdPacketDanjinSecretCoinReduxEventUniv          CmdPacket = 1208
	CmdPacketNewEveryDayDfo                          CmdPacket = 1209
	CmdPacketTechIndexImgResourceOptimizeUniv        CmdPacket = 1210
	CmdPacketTrickOrTreatEventUniv                   CmdPacket = 1211
	CmdPacketOnePlusOneNotTwoEventUniv               CmdPacket = 1212
	CmdPacketArachiEventChangeStateJpn               CmdPacket = 1213
	CmdPacketArachiEventActionJpn                    CmdPacket = 1214
	CmdPacketMissionEventUpdateJpn                   CmdPacket = 1215
	CmdPacketMissionEventRewardJpn                   CmdPacket = 1216
	CmdPacketInvitationOfShusiaReward                CmdPacket = 1217
	CmdPacketInvitationOfShusiaMissionComplete       CmdPacket = 1218
	CmdPacketTerritoryCombatCondition                CmdPacket = 1219
	CmdPacketCeraGetOrSavingChoice                   CmdPacket = 1220
	CmdPacketEventPacketJpn                          CmdPacket = 1221
	CmdPacketEverydayBossTower                       CmdPacket = 1222
	CmdPacketF1PVPAfterReward                        CmdPacket = 1223
	CmdPacketFishingEventAttendance                  CmdPacket = 1224
	CmdPacketTurnOnTheLuckyLamp                      CmdPacket = 1225
	CmdPacketGiftOfSeria                             CmdPacket = 1226
	CmdPacketLetheContract2015                       CmdPacket = 1227
	CmdPacketYundyEvent                              CmdPacket = 1228
	CmdPacketBigTreesmasEventUniv                    CmdPacket = 1229
	CmdPacketDespairTowerEventUniv                   CmdPacket = 1230
	CmdPacketOnYourMarkEventUniv                     CmdPacket = 1231
	CmdPacketAreYouReadyEventUniv                    CmdPacket = 1232
	CmdPacketCharacterDayEventUniv                   CmdPacket = 1233
	CmdPacketPCRoomServiceReqUniv                    CmdPacket = 1234
	CmdPacketValentineDayEventUniv                   CmdPacket = 1235
	CmdPacketCardGame                                CmdPacket = 1236
	CmdPacketCardGameCompound                        CmdPacket = 1237
	CmdPacketGuildModeChangeChn                      CmdPacket = 1238
	CmdPacketCreatureEffectTimeExpire                CmdPacket = 1239
	CmdPacketMirrorAradEventReqUniv                  CmdPacket = 1240
	CmdPacketDnfSchool                               CmdPacket = 1241
	CmdPacketRequestLaundry                          CmdPacket = 1242
	CmdPacketFriendshipHellPartySelect               CmdPacket = 1243
	CmdPacketContractOfGuild                         CmdPacket = 1244
	CmdPacketGrowBeanstalkNPC                        CmdPacket = 1245
	CmdPacketNPCGrowUpUseItem                        CmdPacket = 1246
	CmdPacketP1Tournament                            CmdPacket = 1247
	CmdPacketSercretCoinRouletteAddChanceUniv        CmdPacket = 1248
	CmdPacketSercretCoinRoulettePlayUniv             CmdPacket = 1249
	CmdPacketChoiceRoulette                          CmdPacket = 1250
	CmdPacketNewbieAndRetureneeBonusReward           CmdPacket = 1251
	CmdPacketHalidomRentalReqUniv                    CmdPacket = 1252
	CmdPacketDnfDraftReqUniv                         CmdPacket = 1253
	CmdPacketDnfDraftResponseUniv                    CmdPacket = 1254
	CmdPacketDnfDraftShopPurchaseUniv                CmdPacket = 1255
	CmdPacketDnfDraftTargetStateUniv                 CmdPacket = 1256
	CmdPacketDnfDraftRecommendStateUniv              CmdPacket = 1257
	CmdPacketAppointedDungeonClear                   CmdPacket = 1258
	CmdPacketRealEstateUseShieldItem                 CmdPacket = 1259
	CmdPacketGuessNumber                             CmdPacket = 1260
	CmdPacketFirstLoginRewardPopup                   CmdPacket = 1261
	CmdPacketLogConnectProcess                       CmdPacket = 1262
	CmdPacketSakuraEvent2016                         CmdPacket = 1263
	CmdPacketLuckyBalloon                            CmdPacket = 1264
	CmdPacketFoodFighterDungeon                      CmdPacket = 1265
	CmdPacketArcadePVPDataCopy                       CmdPacket = 1266
	CmdPacketPonguntookukReward                      CmdPacket = 1267
	CmdPacketApcPVPStart                             CmdPacket = 1268
	CmdPacketApcPVPDie                               CmdPacket = 1269
	CmdPacketApcPVPTimeOut                           CmdPacket = 1270
	CmdPacketUsagePVPFatigue                         CmdPacket = 1271
	CmdPacketPVPFatigueReward                        CmdPacket = 1272
	CmdPacketChangePVPPrivateToNomal                 CmdPacket = 1273
	CmdPacketEventMissionForGuildUniv                CmdPacket = 1274
	CmdPacketDnfWithFriendsCharacInfo                CmdPacket = 1275
	CmdPacketCheckUserConnection                     CmdPacket = 1276
	CmdPacketYundyRunGiveRewardUniv                  CmdPacket = 1277
	CmdPacketSnorkelingInfo                          CmdPacket = 1278
	CmdPacketRavenBridge                             CmdPacket = 1279
	CmdPacketEventLuckySevenUniv                     CmdPacket = 1280
	CmdPacketEventDarkElfDungeonUniv                 CmdPacket = 1281
	CmdPacketPotionologyComposeUniv                  CmdPacket = 1282
	CmdPacketPotionologyTryAnswerUniv                CmdPacket = 1283
	CmdPacketSteamRequestDlcPackageItem              CmdPacket = 1284
	CmdPacketArcademodePVPRoundInfo                  CmdPacket = 1285
	CmdPacketTextureMemoryStatistics                 CmdPacket = 1286
	CmdPacketDungeonTextureMemoryStatistics          CmdPacket = 1287
	CmdPacketSelectDamageFontSkin                    CmdPacket = 1288
	CmdPacketSaveDnfPremierLeagueRecord              CmdPacket = 1289
	CmdPacketCircusDungeonReward                     CmdPacket = 1290
	CmdPacketJoustInfo                               CmdPacket = 1291
	CmdPacketJoustBetting                            CmdPacket = 1292
	CmdPacketJoustMatchHistory                       CmdPacket = 1293
	CmdPacketAtDailyAttendance                       CmdPacket = 1294
	CmdPacketAveragePingLogChn                       CmdPacket = 1295
	CmdPacketWarPreparationEventUniv                 CmdPacket = 1296
	CmdPacketAntonRaidEventBuff                      CmdPacket = 1297
	CmdPacketFishingEventUniv                        CmdPacket = 1298
	CmdPacketHalloween2016Chn                        CmdPacket = 1299
	CmdPacketGetGuildHongbaoList                     CmdPacket = 1300
	CmdPacketGetGuildHongbaoPointList                CmdPacket = 1301
	CmdPacketGetGuildHongbaoHistoryList              CmdPacket = 1302
	CmdPacketGiveGuildHongbao                        CmdPacket = 1303
	CmdPacketTakeGuildHongbao                        CmdPacket = 1304
	CmdPacketReqRewardGuildSpecEvent                 CmdPacket = 1305
	CmdPacketReqGuildSpecInfo                        CmdPacket = 1306
	CmdPacketMoveToVillagePrev                       CmdPacket = 1307
	CmdPacketUserCheckstatDistribution               CmdPacket = 1308
	CmdPacketRegisterFreeCashAccount                 CmdPacket = 1309
	CmdPacketFreeCashRewardRemainCount               CmdPacket = 1310
	CmdPacketRequestFreeCashReward                   CmdPacket = 1311
	CmdPacketCircusDungeonUniv                       CmdPacket = 1312
	CmdPacketRequestGaraponOpenJpn                   CmdPacket = 1313
	CmdPacketCharacBuffDayEventUniv                  CmdPacket = 1314
	CmdPacketAvatarConvertWitchesPot                 CmdPacket = 1315
	CmdPacketDoubleupMinigame                        CmdPacket = 1316
	CmdPacketChronicleFullSetEventUniv               CmdPacket = 1317
	CmdPacketPackagebonusSeasonserverUniv            CmdPacket = 1318
	CmdPacketErrorImageListStat                      CmdPacket = 1319
	CmdPacketLogDomainConnect                        CmdPacket = 1320
	CmdPacketJewelryBattleStart                      CmdPacket = 1321
	CmdPacketJewelryBattleJewelryCheck               CmdPacket = 1322
	CmdPacketRequestOfIllusion                       CmdPacket = 1323
	CmdPacketFriendRecommendSetRewardCharac          CmdPacket = 1324
	CmdPacketLetsPlayDfoEventUniv                    CmdPacket = 1325
	CmdPacketRobinRaid                               CmdPacket = 1326
	CmdPacketDetectiveDungeonPuzzle                  CmdPacket = 1327
	CmdPacketAradDetectiveOffice                     CmdPacket = 1328
	CmdPacketJoanMagicalLampRequest                  CmdPacket = 1329
	CmdPacketWishLanterns                            CmdPacket = 1330
	CmdPacketAtPriest2Awakening                      CmdPacket = 1331
	CmdPacketSupportEkern                            CmdPacket = 1332
	CmdPacketUpdateHangSocksCountUniv                CmdPacket = 1333
	CmdPacketSetHangSocksUniv                        CmdPacket = 1334
	CmdPacketHangSocksRewardUniv                     CmdPacket = 1335
	CmdPacketDailyRewardUniv                         CmdPacket = 1336
	CmdPacketBeginning2017EventUniv                  CmdPacket = 1337
	CmdPacketBurningPVPEventUniv                     CmdPacket = 1338
	CmdPacketNeopremiumReformEventUniv               CmdPacket = 1339
	CmdPacketCustomAbilityEquipOption                CmdPacket = 1340
	CmdPacketCustomAbilitySetEquipOption             CmdPacket = 1341
	CmdPacketCustomAbilityUpgrade                    CmdPacket = 1342
	CmdPacketLogAbnormalDamage                       CmdPacket = 1343
	CmdPacketEquipmentMaskingCharacInfo              CmdPacket = 1344
	CmdPacketAgitWarInfo                             CmdPacket = 1345
	CmdPacketAgitWarMissionReward                    CmdPacket = 1346
	CmdPacketVoiceChatMemberInit                     CmdPacket = 1347
	CmdPacketVoiceChatCreateRoom                     CmdPacket = 1348
	CmdPacketVoiceChatRoomList                       CmdPacket = 1349
	CmdPacketAgitWarGuardian                         CmdPacket = 1350
	CmdPacketAgitWarExtend                           CmdPacket = 1351
	CmdPacketUpdateWishItem                          CmdPacket = 1352
	CmdPacketEggWatchPhaseup                         CmdPacket = 1353
	CmdPacketAgitWarShop                             CmdPacket = 1354
	CmdPacketAgitWarDungeonRequirePoint              CmdPacket = 1355
	CmdPacketRequestDungeonDriverCharacter           CmdPacket = 1356
	CmdPacketDecideDungeonDriverCharacter            CmdPacket = 1357
	CmdPacketFramelagPerPVP                          CmdPacket = 1358
	CmdPacketRequestRaidInfo                         CmdPacket = 1359
	CmdPacketAgitWarSeasonReward                     CmdPacket = 1360
	CmdPacketUDPPacketNetworkStatisticPerSec         CmdPacket = 1361
	CmdPacketUDPPacketStatData                       CmdPacket = 1362
	CmdPacketUDPPacketPingPerSize                    CmdPacket = 1363
	CmdPacketAgitWarSelectChallengeGuild             CmdPacket = 1364
	CmdPacketTcpPacketStatData                       CmdPacket = 1365
	CmdPacketSsdUtilizationRate                      CmdPacket = 1366
	CmdPacketRequestRaidEntranceInfo                 CmdPacket = 1367
	CmdPacketStartGentResistance                     CmdPacket = 1368
	CmdPacketEndGentResistance                       CmdPacket = 1369
	CmdPacketRequestWeekendTimeBonus                 CmdPacket = 1370
	CmdPacketUpdateMoonlightTavernSystem             CmdPacket = 1371
	CmdPacketMoonlightTavernMessage                  CmdPacket = 1372
	CmdPacketStartChainRush                          CmdPacket = 1373
	CmdPacketStopChainRush                           CmdPacket = 1374
	CmdPacketSizukiArenaSeason2EventUniv             CmdPacket = 1375
	CmdPacketChocolatierEventMakeReqUniv             CmdPacket = 1376
	CmdPacketChocolatierEventRecvRewardUniv          CmdPacket = 1377
	CmdPacketChocolatierEventReqNotiUniv             CmdPacket = 1378
	CmdPacketLightDarkEventSpecReward                CmdPacket = 1379
	CmdPacketLightDarkTurnEnd                        CmdPacket = 1380
	CmdPacketLightDarkSelectCard                     CmdPacket = 1381
	CmdPacketLightDarkTimeOutPower                   CmdPacket = 1382
	CmdPacketNurtureAccEventCharacRewardUniv         CmdPacket = 1383
	CmdPacketNurtureAccEventRewardUniv               CmdPacket = 1384
	CmdPacketWeeklyAttendanceReward                  CmdPacket = 1385
	CmdPacketDanjinBreakGameStart                    CmdPacket = 1386
	CmdPacketDanjinBreakReward                       CmdPacket = 1387
	CmdPacketFindingNumberResult                     CmdPacket = 1388
	CmdPacketDailyDungeonReward                      CmdPacket = 1389
	CmdPacketDailyDungeonMissionChange               CmdPacket = 1390
	CmdPacketMonsterCardQuizStart                    CmdPacket = 1391
	CmdPacketMonsterCardQuizAnswer                   CmdPacket = 1392
	CmdPacketMonsterCardQuizNext                     CmdPacket = 1393
	CmdPacketSeria2017Reward                         CmdPacket = 1394
	CmdPacketGriefTowerComeOverEvent                 CmdPacket = 1395
	CmdPacketChronicle999Event                       CmdPacket = 1396
	CmdPacketChronicleDonateGoldEvent                CmdPacket = 1397
	CmdPacketOriginPreludeDialog                     CmdPacket = 1398
	CmdPacketOriginPreludeReward                     CmdPacket = 1399
	CmdPacketUIHistoryLogChn                         CmdPacket = 1400
	CmdPacketReqPersonalTrainingCharacter            CmdPacket = 1401
	CmdPacketPartyCorpseHitRenewal                   CmdPacket = 1402
	CmdPacketRequestAdventureInfo                    CmdPacket = 1403
	CmdPacketRemotePeerPacket                        CmdPacket = 1404
	CmdPacketReqRemotePeer                           CmdPacket = 1405
	CmdPacketResRemotePeer                           CmdPacket = 1406
	CmdPacketMercenaryCompetitionCancle              CmdPacket = 1407
	CmdPacketMercenaryCompetitionRewardRequest       CmdPacket = 1408
	CmdPacketMercenaryPointRecalculate               CmdPacket = 1409
	CmdPacketMomentLagStatistic                      CmdPacket = 1410
	CmdPacketBetrayalDungeonAnswer                   CmdPacket = 1411
	CmdPacketRequestPcroomNexonCashEventInfo         CmdPacket = 1412
	CmdPacketRequestDailyGift                        CmdPacket = 1413
	CmdPacketAdventurerShopPurchase                  CmdPacket = 1414
	CmdPacketChildrensdayGiftShootingDelivery        CmdPacket = 1415
	CmdPacketWarriorMaker                            CmdPacket = 1416
	CmdPacketEpicProductionStartFinish               CmdPacket = 1417
	CmdPacketEpicProductionChangeItem                CmdPacket = 1418
	CmdPacketEpicProductionProcess                   CmdPacket = 1419
	CmdPacketEpicProductionMaterialCompound          CmdPacket = 1420
	CmdPacketEpicProductionAbilityOption             CmdPacket = 1421
	CmdPacketAutoRegisterEventCharacter              CmdPacket = 1422
	CmdPacketSummer2017                              CmdPacket = 1423
	CmdPacketDailyAttendanceCheckReq                 CmdPacket = 1424
	CmdPacketPrevVillage                             CmdPacket = 1425
	CmdPacketFpsDevideCollect                        CmdPacket = 1426
	CmdPacketRequestCardPick                         CmdPacket = 1427
	CmdPacketSkillSwitchInventory                    CmdPacket = 1428
	CmdPacketClearQuestTicket                        CmdPacket = 1429
	CmdPacketClearBranchQuest                        CmdPacket = 1430
	CmdPacketNewbieGuideOption                       CmdPacket = 1431
	CmdPacketNewbieMissionReward                     CmdPacket = 1432
	CmdPacketSelectCardSkip                          CmdPacket = 1433
	CmdPacketRequestOriginReturnUserReward           CmdPacket = 1434
	CmdPacketRecommendOriginReturnUser               CmdPacket = 1435
	CmdPacketRequestOriginRecommendReward            CmdPacket = 1436
	CmdPacketUpdateRepresentAccountName              CmdPacket = 1437
	CmdPacketAccountFriendAddRequest                 CmdPacket = 1438
	CmdPacketAccountFriendAccept                     CmdPacket = 1439
	CmdPacketAccountFriendDelete                     CmdPacket = 1440
	CmdPacketAccountFriendRefuseCancel               CmdPacket = 1441
	CmdPacketUpdateAccountFriendInfo                 CmdPacket = 1442
	CmdPacketRepresentAccountNameDuplicateCheck      CmdPacket = 1443
	CmdPacketChangeRepresentAccountName              CmdPacket = 1444
	CmdPacketStoryDigestUpdate                       CmdPacket = 1445
	CmdPacketSaveRecentEmoticonList                  CmdPacket = 1446
	CmdPacketEventBattleshipDungeon                  CmdPacket = 1447
	CmdPacketJulySeventhReward                       CmdPacket = 1448
	CmdPacketOneColorBall                            CmdPacket = 1449
	CmdPacketReqGrainCombination                     CmdPacket = 1450
	CmdPacketNgsSecurityData                         CmdPacket = 1451
	CmdPacketSearchGuildList                         CmdPacket = 1452
	CmdPacketRequestAccountGuildList                 CmdPacket = 1453
	CmdPacketGuildAllMemberGrade                     CmdPacket = 1454
	CmdPacketGuildAllMemberGradeNext                 CmdPacket = 1455
	CmdPacketShowroomAvatarRent                      CmdPacket = 1457
	CmdPacketQuestReplay                             CmdPacket = 1458
	CmdPacketUpdateTagTournamentCharacter            CmdPacket = 1459
	CmdPacketTagTournamentCharacterTagIn             CmdPacket = 1460
	CmdPacketDieTagTournamentCharacter               CmdPacket = 1461
	CmdPacketDatingSimulation                        CmdPacket = 1462
	CmdPacketBeLegendEvent                           CmdPacket = 1463
	CmdPacketBroadcastVoiceChatStatus                CmdPacket = 1464
	CmdPacketReqAccumulteAttendanceReward            CmdPacket = 1465
	CmdPacketPresentOfAnubyReward                    CmdPacket = 1466
	CmdPacketGetRepresentCharacJob                   CmdPacket = 1467
	CmdPacketSetRepresentCharacJob                   CmdPacket = 1468
	CmdPacketRemoveRepresentCharacJob                CmdPacket = 1469
	CmdPacketFakeLoginOpenSpaceSystemTest            CmdPacket = 1470
	CmdPacketHitRandombox                            CmdPacket = 1471
	CmdPacketKeepCalmAndRodeoStartGame               CmdPacket = 1472
	CmdPacketBangBangBangStartGame                   CmdPacket = 1473
	CmdPacketEpicChristmasEvent                      CmdPacket = 1474
	CmdPacketMinority2017Vote                        CmdPacket = 1475
	CmdPacketMinority2017Info                        CmdPacket = 1476
	CmdPacketMinority2017Reward                      CmdPacket = 1477
	CmdPacketTurnHellRoulette                        CmdPacket = 1478
	CmdPacketCardBattleMoveCard                      CmdPacket = 1479
	CmdPacketCardBattleThrow                         CmdPacket = 1480
	CmdPacketCardBattleGiveup                        CmdPacket = 1481
	CmdPacketCardBattleCompound                      CmdPacket = 1482
	CmdPacketCardBattleAiMode                        CmdPacket = 1483
	CmdPacketPickAddChanceItem                       CmdPacket = 1484
	CmdPacketEggWatchCureState                       CmdPacket = 1485
	CmdPacketRedEnvelope                             CmdPacket = 1486
	CmdPacketRedEnvelopeAccumulateReward             CmdPacket = 1487
	CmdPacketAvatarFittingRoomChange                 CmdPacket = 1488
	CmdPacketBeastSoul                               CmdPacket = 1489
	CmdPacketReportBeastMonsterHp                    CmdPacket = 1490
	CmdPacketAdventureGrowthcapsuleExp               CmdPacket = 1491
	CmdPacketCheeryBlossomSightseeing                CmdPacket = 1492
	CmdPacketWomensDay                               CmdPacket = 1493
	CmdPacketReqAllservergroupLimitItemCount         CmdPacket = 1494
	CmdPacketMermaidStarLiveReward                   CmdPacket = 1495
	CmdPacketChangeDisguise                          CmdPacket = 1496
	CmdPacketOutsideGameReward                       CmdPacket = 1497
	CmdPacketHellPartyLiver                          CmdPacket = 1498
	CmdPacketBattleRoyalInfo                         CmdPacket = 1499
	CmdPacketRequestPlantTree                        CmdPacket = 1500
	CmdPacketTwentiethMayValentineDay                CmdPacket = 1501
	CmdPacketLabordayPuzzle                          CmdPacket = 1502
	CmdPacketEventMahjongJpn                         CmdPacket = 1503
	CmdPacketMakingSandwichStart                     CmdPacket = 1504
	CmdPacketMakingSandwichCheck                     CmdPacket = 1505
	CmdPacketWhacGameEnd                             CmdPacket = 1506
	CmdPacketTencentPCRoomLoginReward                CmdPacket = 1507
	CmdPacketGrantVoiceChat                          CmdPacket = 1508
	CmdPacketOldUserFirstLoginRewardPopup            CmdPacket = 1509
	CmdPacketLetsNewPickPresent                      CmdPacket = 1510
	CmdPacketFiendWarBossProc                        CmdPacket = 1511
	CmdPacketLionsMinigameStart                      CmdPacket = 1512
	CmdPacketLionsDinnerBuff                         CmdPacket = 1513
	CmdPacketTakeAPictureStep                        CmdPacket = 1514
	CmdPacketFind7goldBullion                        CmdPacket = 1515
	CmdPacketAntibot                                 CmdPacket = 1516
	CmdPacketDproto                                  CmdPacket = 1517
	CmdPacketDprotoCallback                          CmdPacket = 1518
	CmdPacketEnd                                     CmdPacket = 1519
)

const (
	CmdPacketMaxValue uint16 = uint16(CmdPacketEnd)
)

// CmdPacketName 返回 runtime 表里的原始命令名，未知值返回 ENUM_CMDPACKET_UNKNOWN。
func CmdPacketName(opcode uint16) string {
	switch opcode {
	case uint16(CmdPacketCheckConnection):
		return "ENUM_CMDPACKET_CHECK_CONNECTION"
	case uint16(CmdPacketLogin):
		return "ENUM_CMDPACKET_LOGIN"
	case uint16(CmdPacketSetUDPIPPort):
		return "ENUM_CMDPACKET_SET_UDP_IP_PORT"
	case uint16(CmdPacketExit):
		return "ENUM_CMDPACKET_EXIT"
	case uint16(CmdPacketSelectCharacter):
		return "ENUM_CMDPACKET_SELECT_CHARACTER"
	case uint16(CmdPacketCreateCharacter):
		return "ENUM_CMDPACKET_CREATE_CHARACTER"
	case uint16(CmdPacketDeleteCharacter):
		return "ENUM_CMDPACKET_DELETE_CHARACTER"
	case uint16(CmdPacketReturnSelectCharacter):
		return "ENUM_CMDPACKET_RETURN_SELECT_CHARACTER"
	case uint16(CmdPacketGetUserinfo):
		return "ENUM_CMDPACKET_GET_USERINFO"
	case uint16(CmdPacketRecoverStamina):
		return "ENUM_CMDPACKET_RECOVER_STAMINA"
	case uint16(CmdPacketRequestPeer):
		return "ENUM_CMDPACKET_REQUEST_PEER"
	case uint16(CmdPacketResponsePeer):
		return "ENUM_CMDPACKET_RESPONSE_PEER"
	case uint16(CmdPacketSetPartyInfo):
		return "ENUM_CMDPACKET_SET_PARTY_INFO"
	case uint16(CmdPacketLeaveParty):
		return "ENUM_CMDPACKET_LEAVE_PARTY"
	case uint16(CmdPacketWalkoutPartyMember):
		return "ENUM_CMDPACKET_WALKOUT_PARTY_MEMBER"
	case uint16(CmdPacketEnterSelectDungeon):
		return "ENUM_CMDPACKET_ENTER_SELECT_DUNGEON"
	case uint16(CmdPacketSelectDungeon):
		return "ENUM_CMDPACKET_SELECT_DUNGEON"
	case uint16(CmdPacketSendMessage):
		return "ENUM_CMDPACKET_SEND_MESSAGE"
	case uint16(CmdPacketDeleteItem):
		return "ENUM_CMDPACKET_DELETE_ITEM"
	case uint16(CmdPacketMoveItemspace):
		return "ENUM_CMDPACKET_MOVE_ITEMSPACE"
	case uint16(CmdPacketSortItem):
		return "ENUM_CMDPACKET_SORT_ITEM"
	case uint16(CmdPacketBuyItem):
		return "ENUM_CMDPACKET_BUY_ITEM"
	case uint16(CmdPacketSellItem):
		return "ENUM_CMDPACKET_SELL_ITEM"
	case uint16(CmdPacketRepairEquipment):
		return "ENUM_CMDPACKET_REPAIR_EQUIPMENT"
	case uint16(CmdPacketSetItemtradeState):
		return "ENUM_CMDPACKET_SET_ITEMTRADE_STATE"
	case uint16(CmdPacketCompoundItem):
		return "ENUM_CMDPACKET_COMPOUND_ITEM"
	case uint16(CmdPacketDisjointItem):
		return "ENUM_CMDPACKET_DISJOINT_ITEM"
	case uint16(CmdPacketUseLotteryItem):
		return "ENUM_CMDPACKET_USE_LOTTERY_ITEM"
	case uint16(CmdPacketChangeSkillslot):
		return "ENUM_CMDPACKET_CHANGE_SKILLSLOT"
	case uint16(CmdPacketBuySkill):
		return "ENUM_CMDPACKET_BUY_SKILL"
	case uint16(CmdPacketIncreaseStatus):
		return "ENUM_CMDPACKET_INCREASE_STATUS"
	case uint16(CmdPacketAcceptQuest):
		return "ENUM_CMDPACKET_ACCEPT_QUEST"
	case uint16(CmdPacketGiveupQuest):
		return "ENUM_CMDPACKET_GIVEUP_QUEST"
	case uint16(CmdPacketSetQuestTrigger):
		return "ENUM_CMDPACKET_SET_QUEST_TRIGGER"
	case uint16(CmdPacketFinishQuest):
		return "ENUM_CMDPACKET_FINISH_QUEST"
	case uint16(CmdPacketSetUserPosition):
		return "ENUM_CMDPACKET_SET_USER_POSITION"
	case uint16(CmdPacketSetUserArea):
		return "ENUM_CMDPACKET_SET_USER_AREA"
	case uint16(CmdPacketFinishLoading):
		return "ENUM_CMDPACKET_FINISH_LOADING"
	case uint16(CmdPacketUseSkill):
		return "ENUM_CMDPACKET_USE_SKILL"
	case uint16(CmdPacketDieMonster):
		return "ENUM_CMDPACKET_DIE_MONSTER"
	case uint16(CmdPacketDieCharacter):
		return "ENUM_CMDPACKET_DIE_CHARACTER"
	case uint16(CmdPacketUseCoin):
		return "ENUM_CMDPACKET_USE_COIN"
	case uint16(CmdPacketGiveupGame):
		return "ENUM_CMDPACKET_GIVEUP_GAME"
	case uint16(CmdPacketGetItem):
		return "ENUM_CMDPACKET_GET_ITEM"
	case uint16(CmdPacketUseStackable):
		return "ENUM_CMDPACKET_USE_STACKABLE"
	case uint16(CmdPacketMoveMap):
		return "ENUM_CMDPACKET_MOVE_MAP"
	case uint16(CmdPacketSetPlayResult):
		return "ENUM_CMDPACKET_SET_PLAY_RESULT"
	case uint16(CmdPacketDropItem):
		return "ENUM_CMDPACKET_DROP_ITEM"
	case uint16(CmdPacketDecreaseDurability):
		return "ENUM_CMDPACKET_DECREASE_DURABILITY"
	case uint16(CmdPacketReportBadP2pUser):
		return "ENUM_CMDPACKET_REPORT_BAD_P2P_USER"
	case uint16(CmdPacketMakePVPRoom):
		return "ENUM_CMDPACKET_MAKE_PVP_ROOM"
	case uint16(CmdPacketEnterPVPRoom):
		return "ENUM_CMDPACKET_ENTER_PVP_ROOM"
	case uint16(CmdPacketSetPVPSeatState):
		return "ENUM_CMDPACKET_SET_PVP_SEAT_STATE"
	case uint16(CmdPacketSetPVPReadyState):
		return "ENUM_CMDPACKET_SET_PVP_READY_STATE"
	case uint16(CmdPacketSetPVPTeamMode):
		return "ENUM_CMDPACKET_SET_PVP_TEAM_MODE"
	case uint16(CmdPacketDiePVPCharacter):
		return "ENUM_CMDPACKET_DIE_PVP_CHARACTER"
	case uint16(CmdPacketPVPTimeOut):
		return "ENUM_CMDPACKET_PVP_TIME_OUT"
	case uint16(CmdPacketEndPVPResult):
		return "ENUM_CMDPACKET_END_PVP_RESULT"
	case uint16(CmdPacketResPVPRank):
		return "ENUM_CMDPACKET_RES_PVP_RANK"
	case uint16(CmdPacketSetPVPMapIndex):
		return "ENUM_CMDPACKET_SET_PVP_MAP_INDEX"
	case uint16(CmdPacketAddFriend):
		return "ENUM_CMDPACKET_ADD_FRIEND"
	case uint16(CmdPacketRemoveFriend):
		return "ENUM_CMDPACKET_REMOVE_FRIEND"
	case uint16(CmdPacketDebugCommand):
		return "ENUM_CMDPACKET_DEBUG_COMMAND"
	case uint16(CmdPacketCera):
		return "ENUM_CMDPACKET_CERA"
	case uint16(CmdPacketBuyCerashopItem):
		return "ENUM_CMDPACKET_BUY_CERASHOP_ITEM"
	case uint16(CmdPacketGenCeraticket):
		return "ENUM_CMDPACKET_GEN_CERATICKET"
	case uint16(CmdPacketRequestPvpexpOfWeek):
		return "ENUM_CMDPACKET_REQUEST_PVPEXP_OF_WEEK"
	case uint16(CmdPacketGuildMemerList):
		return "ENUM_CMDPACKET_GUILD_MEMER_LIST"
	case uint16(CmdPacketCallGuildCreateRight):
		return "ENUM_CMDPACKET_CALL_GUILD_CREATE_RIGHT"
	case uint16(CmdPacketScoreScrollState):
		return "ENUM_CMDPACKET_SCORE_SCROLL_STATE"
	case uint16(CmdPacketCardSelectRightState):
		return "ENUM_CMDPACKET_CARD_SELECT_RIGHT_STATE"
	case uint16(CmdPacketSelectCard):
		return "ENUM_CMDPACKET_SELECT_CARD"
	case uint16(CmdPacketEplpCommand):
		return "ENUM_CMDPACKET_EPLP_COMMAND"
	case uint16(CmdPacketCallGuildLevelUp):
		return "ENUM_CMDPACKET_CALL_GUILD_LEVEL_UP"
	case uint16(CmdPacketGuildInfo):
		return "ENUM_CMDPACKET_GUILD_INFO"
	case uint16(CmdPacketRequestGuildEnter):
		return "ENUM_CMDPACKET_REQUEST_GUILD_ENTER"
	case uint16(CmdPacketRequestMemberEnter):
		return "ENUM_CMDPACKET_REQUEST_MEMBER_ENTER"
	case uint16(CmdPacketMemberEnterReply):
		return "ENUM_CMDPACKET_MEMBER_ENTER_REPLY"
	case uint16(CmdPacketMemberSecede):
		return "ENUM_CMDPACKET_MEMBER_SECEDE"
	case uint16(CmdPacketCallMemerList):
		return "ENUM_CMDPACKET_CALL_MEMER_LIST"
	case uint16(CmdPacketUpgradeItem):
		return "ENUM_CMDPACKET_UPGRADE_ITEM"
	case uint16(CmdPacketResetItemAttr):
		return "ENUM_CMDPACKET_RESET_ITEM_ATTR"
	case uint16(CmdPacketBuyPrivateStoreItem):
		return "ENUM_CMDPACKET_BUY_PRIVATE_STORE_ITEM"
	case uint16(CmdPacketEnterPrivateStore):
		return "ENUM_CMDPACKET_ENTER_PRIVATE_STORE"
	case uint16(CmdPacketExitPrivateStore):
		return "ENUM_CMDPACKET_EXIT_PRIVATE_STORE"
	case uint16(CmdPacketCreatePrivateStore):
		return "ENUM_CMDPACKET_CREATE_PRIVATE_STORE"
	case uint16(CmdPacketRemovePrivateStore):
		return "ENUM_CMDPACKET_REMOVE_PRIVATE_STORE"
	case uint16(CmdPacketCompleteDisplay):
		return "ENUM_CMDPACKET_COMPLETE_DISPLAY"
	case uint16(CmdPacketMoveToGate):
		return "ENUM_CMDPACKET_MOVE_TO_GATE"
	case uint16(CmdPacketMakeWarroomTemp):
		return "ENUM_CMDPACKET_MAKE_WARROOM_TEMP"
	case uint16(CmdPacketEnterWarroom):
		return "ENUM_CMDPACKET_ENTER_WARROOM"
	case uint16(CmdPacketSetWarroomSeatState):
		return "ENUM_CMDPACKET_SET_WARROOM_SEAT_STATE"
	case uint16(CmdPacketDieWarroomCharacter):
		return "ENUM_CMDPACKET_DIE_WARROOM_CHARACTER"
	case uint16(CmdPacketStartWarroomTemp):
		return "ENUM_CMDPACKET_START_WARROOM_TEMP"
	case uint16(CmdPacketMailboxSend):
		return "ENUM_CMDPACKET_MAILBOX_SEND"
	case uint16(CmdPacketMailboxExtractItem):
		return "ENUM_CMDPACKET_MAILBOX_EXTRACT_ITEM"
	case uint16(CmdPacketMailboxOpen):
		return "ENUM_CMDPACKET_MAILBOX_OPEN"
	case uint16(CmdPacketPeerConnectResult):
		return "ENUM_CMDPACKET_PEER_CONNECT_RESULT"
	case uint16(CmdPacketQuickJoinRoom):
		return "ENUM_CMDPACKET_QUICK_JOIN_ROOM"
	case uint16(CmdPacketCompoundAvatar):
		return "ENUM_CMDPACKET_COMPOUND_AVATAR"
	case uint16(CmdPacketRenameCreature):
		return "ENUM_CMDPACKET_RENAME_CREATURE"
	case uint16(CmdPacketResponseCreature):
		return "ENUM_CMDPACKET_RESPONSE_CREATURE"
	case uint16(CmdPacketHatchCreature):
		return "ENUM_CMDPACKET_HATCH_CREATURE"
	case uint16(CmdPacketBuyAutomatItem):
		return "ENUM_CMDPACKET_BUY_AUTOMAT_ITEM"
	case uint16(CmdPacketRequestAvagachaCoupon):
		return "ENUM_CMDPACKET_REQUEST_AVAGACHA_COUPON"
	case uint16(CmdPacketGatheringPartyStatus):
		return "ENUM_CMDPACKET_GATHERING_PARTY_STATUS"
	case uint16(CmdPacketWorldCupHitCount):
		return "ENUM_CMDPACKET_WORLD_CUP_HIT_COUNT"
	case uint16(CmdPacketGMCommand):
		return "ENUM_CMDPACKET_GM_COMMAND"
	case uint16(CmdPacketReport4Hack):
		return "ENUM_CMDPACKET_REPORT_4_HACK"
	case uint16(CmdPacketGuildWarInfo):
		return "ENUM_CMDPACKET_GUILD_WAR_INFO"
	case uint16(CmdPacketPVPHeartBeat):
		return "ENUM_CMDPACKET_PVP_HEART_BEAT"
	case uint16(CmdPacketCodeChecksum):
		return "ENUM_CMDPACKET_CODE_CHECKSUM"
	case uint16(CmdPacketPVPRequestFight):
		return "ENUM_CMDPACKET_PVP_REQUEST_FIGHT"
	case uint16(CmdPacketMouseregister):
		return "ENUM_CMDPACKET_MOUSEREGISTER"
	case uint16(CmdPacketCreatureSendMessage):
		return "ENUM_CMDPACKET_CREATURE_SEND_MESSAGE"
	case uint16(CmdPacketTraceError):
		return "ENUM_CMDPACKET_TRACE_ERROR"
	case uint16(CmdPacketOtherUserInfo):
		return "ENUM_CMDPACKET_OTHER_USER_INFO"
	case uint16(CmdPacketBossDieCheck):
		return "ENUM_CMDPACKET_BOSS_DIE_CHECK"
	case uint16(CmdPacketRegisiterToBlacklist):
		return "ENUM_CMDPACKET_REGISITER_TO_BLACKLIST"
	case uint16(CmdPacketDeleteToBlacklist):
		return "ENUM_CMDPACKET_DELETE_TO_BLACKLIST"
	case uint16(CmdPacketRequestBlacklist):
		return "ENUM_CMDPACKET_REQUEST_BLACKLIST"
	case uint16(CmdPacketChangeHost):
		return "ENUM_CMDPACKET_CHANGE_HOST"
	case uint16(CmdPacketCreatureScriptMessage):
		return "ENUM_CMDPACKET_CREATURE_SCRIPT_MESSAGE"
	case uint16(CmdPacketCharacterStatistic):
		return "ENUM_CMDPACKET_CHARACTER_STATISTIC"
	case uint16(CmdPacketReportClientSpec):
		return "ENUM_CMDPACKET_REPORT_CLIENT_SPEC"
	case uint16(CmdPacketGuildmemberNaming):
		return "ENUM_CMDPACKET_GUILDMEMBER_NAMING"
	case uint16(CmdPacketSetSubGuildMaster):
		return "ENUM_CMDPACKET_SET_SUB_GUILD_MASTER"
	case uint16(CmdPacketExchangeServerInfo):
		return "ENUM_CMDPACKET_EXCHANGE_SERVER_INFO"
	case uint16(CmdPacketExchangeServerInfoRet):
		return "ENUM_CMDPACKET_EXCHANGE_SERVER_INFO_RET"
	case uint16(CmdPacketExchangeServerCharacInfo):
		return "ENUM_CMDPACKET_EXCHANGE_SERVER_CHARAC_INFO"
	case uint16(CmdPacketExchangeServerCharacInfoRet):
		return "ENUM_CMDPACKET_EXCHANGE_SERVER_CHARAC_INFO_RET"
	case uint16(CmdPacketTimeCheck):
		return "ENUM_CMDPACKET_TIME_CHECK"
	case uint16(CmdPacketBack2Village):
		return "ENUM_CMDPACKET_BACK_2_VILLAGE"
	case uint16(CmdPacketDnfRadioListen):
		return "ENUM_CMDPACKET_DNF_RADIO_LISTEN"
	case uint16(CmdPacketChangeLetterStat):
		return "ENUM_CMDPACKET_CHANGE_LETTER_STAT"
	case uint16(CmdPacketChangeCharacName):
		return "ENUM_CMDPACKET_CHANGE_CHARAC_NAME"
	case uint16(CmdPacketQueryCharacInfo):
		return "ENUM_CMDPACKET_QUERY_CHARAC_INFO"
	case uint16(CmdPacketReportMannerlessUser):
		return "ENUM_CMDPACKET_REPORT_MANNERLESS_USER"
	case uint16(CmdPacketAlldieMonster):
		return "ENUM_CMDPACKET_ALLDIE_MONSTER"
	case uint16(CmdPacketGuildMemerListNext):
		return "ENUM_CMDPACKET_GUILD_MEMER_LIST_NEXT"
	case uint16(CmdPacketGuildAllMemberList):
		return "ENUM_CMDPACKET_GUILD_ALL_MEMBER_LIST"
	case uint16(CmdPacketGuildAllMemberListNext):
		return "ENUM_CMDPACKET_GUILD_ALL_MEMBER_LIST_NEXT"
	case uint16(CmdPacketRpyHumanCertify):
		return "ENUM_CMDPACKET_RPY_HUMAN_CERTIFY"
	case uint16(CmdPacketChangeTutorialFlag):
		return "ENUM_CMDPACKET_CHANGE_TUTORIAL_FLAG"
	case uint16(CmdPacketDieAiCharacter):
		return "ENUM_CMDPACKET_DIE_AI_CHARACTER"
	case uint16(CmdPacketCompleteLoadAssault):
		return "ENUM_CMDPACKET_COMPLETE_LOAD_ASSAULT"
	case uint16(CmdPacketConnectP2pAssault):
		return "ENUM_CMDPACKET_CONNECT_P2P_ASSAULT"
	case uint16(CmdPacketDieAssaultPlayer):
		return "ENUM_CMDPACKET_DIE_ASSAULT_PLAYER"
	case uint16(CmdPacketRevivalAssaultPlayer):
		return "ENUM_CMDPACKET_REVIVAL_ASSAULT_PLAYER"
	case uint16(CmdPacketChangeHp):
		return "ENUM_CMDPACKET_CHANGE_HP"
	case uint16(CmdPacketBvhackinfo):
		return "ENUM_CMDPACKET_BVHACKINFO"
	case uint16(CmdPacketCallGuildInvite):
		return "ENUM_CMDPACKET_CALL_GUILD_INVITE"
	case uint16(CmdPacketReplyGuildInvite):
		return "ENUM_CMDPACKET_REPLY_GUILD_INVITE"
	case uint16(CmdPacketReqGuildSecede):
		return "ENUM_CMDPACKET_REQ_GUILD_SECEDE"
	case uint16(CmdPacketNotifyMessageToGuild):
		return "ENUM_CMDPACKET_NOTIFY_MESSAGE_TO_GUILD"
	case uint16(CmdPacketGuildMasterDelegate):
		return "ENUM_CMDPACKET_GUILD_MASTER_DELEGATE"
	case uint16(CmdPacketCheckGuildNameDouble):
		return "ENUM_CMDPACKET_CHECK_GUILD_NAME_DOUBLE"
	case uint16(CmdPacketCheckGuildAddreassDouble):
		return "ENUM_CMDPACKET_CHECK_GUILD_ADDREASS_DOUBLE"
	case uint16(CmdPacketOpenGuildCreateWindow):
		return "ENUM_CMDPACKET_OPEN_GUILD_CREATE_WINDOW"
	case uint16(CmdPacketDeathTowerStageCmd):
		return "ENUM_CMDPACKET_DEATH_TOWER_STAGE_CMD"
	case uint16(CmdPacketUseBoosterItem):
		return "ENUM_CMDPACKET_USE_BOOSTER_ITEM"
	case uint16(CmdPacketSecurityCardIssue):
		return "ENUM_CMDPACKET_SECURITY_CARD_ISSUE"
	case uint16(CmdPacketSecurityCardDisuse):
		return "ENUM_CMDPACKET_SECURITY_CARD_DISUSE"
	case uint16(CmdPacketSecurityCardAuthReq):
		return "ENUM_CMDPACKET_SECURITY_CARD_AUTH_REQ"
	case uint16(CmdPacketSecurityCardAuthRpy):
		return "ENUM_CMDPACKET_SECURITY_CARD_AUTH_RPY"
	case uint16(CmdPacketSecurityCardCertKey):
		return "ENUM_CMDPACKET_SECURITY_CARD_CERT_KEY"
	case uint16(CmdPacketCallPartyMemberRealtimeInfo):
		return "ENUM_CMDPACKET_CALL_PARTY_MEMBER_REALTIME_INFO"
	case uint16(CmdPacketEvadeAssault):
		return "ENUM_CMDPACKET_EVADE_ASSAULT"
	case uint16(CmdPacketAgreeEnchant):
		return "ENUM_CMDPACKET_AGREE_ENCHANT"
	case uint16(CmdPacketTryEnchant):
		return "ENUM_CMDPACKET_TRY_ENCHANT"
	case uint16(CmdPacketPutItemForEnchant):
		return "ENUM_CMDPACKET_PUT_ITEM_FOR_ENCHANT"
	case uint16(CmdPacketClientSpecStatistic):
		return "ENUM_CMDPACKET_CLIENT_SPEC_STATISTIC"
	case uint16(CmdPacketSecurityCardAuthCancel):
		return "ENUM_CMDPACKET_SECURITY_CARD_AUTH_CANCEL"
	case uint16(CmdPacketHatchCreatureEgg):
		return "ENUM_CMDPACKET_HATCH_CREATURE_EGG"
	case uint16(CmdPacketRequestHatchedCreature):
		return "ENUM_CMDPACKET_REQUEST_HATCHED_CREATURE"
	case uint16(CmdPacketRequestCreatureCoupon):
		return "ENUM_CMDPACKET_REQUEST_CREATURE_COUPON"
	case uint16(CmdPacketGmdebugCommand):
		return "ENUM_CMDPACKET_GMDEBUG_COMMAND"
	case uint16(CmdPacketJoinPower):
		return "ENUM_CMDPACKET_JOIN_POWER"
	case uint16(CmdPacketSecedePower):
		return "ENUM_CMDPACKET_SECEDE_POWER"
	case uint16(CmdPacketChangeGuildName):
		return "ENUM_CMDPACKET_CHANGE_GUILD_NAME"
	case uint16(CmdPacketSdcDamageCheck):
		return "ENUM_CMDPACKET_SDC_DAMAGE_CHECK"
	case uint16(CmdPacketSdcActivestatusCheck):
		return "ENUM_CMDPACKET_SDC_ACTIVESTATUS_CHECK"
	case uint16(CmdPacketAuctionAskAveragePrice):
		return "ENUM_CMDPACKET_AUCTION_ASK_AVERAGE_PRICE"
	case uint16(CmdPacketAuctionRegistItem):
		return "ENUM_CMDPACKET_AUCTION_REGIST_ITEM"
	case uint16(CmdPacketAuctionRegistCancel):
		return "ENUM_CMDPACKET_AUCTION_REGIST_CANCEL"
	case uint16(CmdPacketAuctionBidding):
		return "ENUM_CMDPACKET_AUCTION_BIDDING"
	case uint16(CmdPacketAuctionSearchByItemkey):
		return "ENUM_CMDPACKET_AUCTION_SEARCH_BY_ITEMKEY"
	case uint16(CmdPacketAuctionSearchByNoitemkey):
		return "ENUM_CMDPACKET_AUCTION_SEARCH_BY_NOITEMKEY"
	case uint16(CmdPacketAuctionMyRegistedItemInfo):
		return "ENUM_CMDPACKET_AUCTION_MY_REGISTED_ITEM_INFO"
	case uint16(CmdPacketAuctionMyBiddingInfo):
		return "ENUM_CMDPACKET_AUCTION_MY_BIDDING_INFO"
	case uint16(CmdPacketAuctionMyAuctionHistory):
		return "ENUM_CMDPACKET_AUCTION_MY_AUCTION_HISTORY"
	case uint16(CmdPacketDungeonEventStoryPause):
		return "ENUM_CMDPACKET_DUNGEON_EVENT_STORY_PAUSE"
	case uint16(CmdPacketJoinPowerWar):
		return "ENUM_CMDPACKET_JOIN_POWER_WAR"
	case uint16(CmdPacketGoblinPadStatus):
		return "ENUM_CMDPACKET_GOBLIN_PAD_STATUS"
	case uint16(CmdPacketFrameLagStatistics):
		return "ENUM_CMDPACKET_FRAME_LAG_STATISTICS"
	case uint16(CmdPacketPVPChannelInfo):
		return "ENUM_CMDPACKET_PVP_CHANNEL_INFO"
	case uint16(CmdPacketRequestMatch):
		return "ENUM_CMDPACKET_REQUEST_MATCH"
	case uint16(CmdPacketSaveGameOption1):
		return "ENUM_CMDPACKET_SAVE_GAME_OPTION_1"
	case uint16(CmdPacketSaveGameOption2):
		return "ENUM_CMDPACKET_SAVE_GAME_OPTION_2"
	case uint16(CmdPacketSecurityCardRetransfer):
		return "ENUM_CMDPACKET_SECURITY_CARD_RETRANSFER"
	case uint16(CmdPacketCeraIdentify):
		return "ENUM_CMDPACKET_CERA_IDENTIFY"
	case uint16(CmdPacketUseEmblem):
		return "ENUM_CMDPACKET_USE_EMBLEM"
	case uint16(CmdPacketDisjointAvatar):
		return "ENUM_CMDPACKET_DISJOINT_AVATAR"
	case uint16(CmdPacketBeijingOlympicHitCount):
		return "ENUM_CMDPACKET_BEIJING_OLYMPIC_HIT_COUNT"
	case uint16(CmdPacketPurifyItem):
		return "ENUM_CMDPACKET_PURIFY_ITEM"
	case uint16(CmdPacketInvestItemAmplifyOption):
		return "ENUM_CMDPACKET_INVEST_ITEM_AMPLIFY_OPTION"
	case uint16(CmdPacketAddAvatarSocket):
		return "ENUM_CMDPACKET_ADD_AVATAR_SOCKET"
	case uint16(CmdPacketShopCoinEvent):
		return "ENUM_CMDPACKET_SHOP_COIN_EVENT"
	case uint16(CmdPacketUseRandomboxItem):
		return "ENUM_CMDPACKET_USE_RANDOMBOX_ITEM"
	case uint16(CmdPacketUDPCharacteristic):
		return "ENUM_CMDPACKET_UDP_CHARACTERISTIC"
	case uint16(CmdPacketOnedayLethe):
		return "ENUM_CMDPACKET_ONEDAY_LETHE"
	case uint16(CmdPacketDisguiseRequest):
		return "ENUM_CMDPACKET_DISGUISE_REQUEST"
	case uint16(CmdPacketDisguiseCancel):
		return "ENUM_CMDPACKET_DISGUISE_CANCEL"
	case uint16(CmdPacketRequestPCRoomPlayerList):
		return "ENUM_CMDPACKET_REQUEST_PC_ROOM_PLAYER_LIST"
	case uint16(CmdPacketRequestPCRoomPlayerCount):
		return "ENUM_CMDPACKET_REQUEST_PC_ROOM_PLAYER_COUNT"
	case uint16(CmdPacketUseVendingMachine):
		return "ENUM_CMDPACKET_USE_VENDING_MACHINE"
	case uint16(CmdPacketAssertInfo):
		return "ENUM_CMDPACKET_ASSERT_INFO"
	case uint16(CmdPacketOverflowInfo):
		return "ENUM_CMDPACKET_OVERFLOW_INFO"
	case uint16(CmdPacketServerMessageSend):
		return "ENUM_CMDPACKET_SERVER_MESSAGE_SEND"
	case uint16(CmdPacketServerMessageCheck):
		return "ENUM_CMDPACKET_SERVER_MESSAGE_CHECK"
	case uint16(CmdPacketTestHackshieldRequest):
		return "ENUM_CMDPACKET_TEST_HACKSHIELD_REQUEST"
	case uint16(CmdPacketHackshieldClientResponse):
		return "ENUM_CMDPACKET_HACKSHIELD_CLIENT_RESPONSE"
	case uint16(CmdPacketGiveGiftToNPC):
		return "ENUM_CMDPACKET_GIVE_GIFT_TO_NPC"
	case uint16(CmdPacketGoblinPadResponseCryptKey):
		return "ENUM_CMDPACKET_GOBLIN_PAD_RESPONSE_CRYPT_KEY"
	case uint16(CmdPacketWriteGuildMemberMemo):
		return "ENUM_CMDPACKET_WRITE_GUILD_MEMBER_MEMO"
	case uint16(CmdPacketSetPVPGold):
		return "ENUM_CMDPACKET_SET_PVP_GOLD"
	case uint16(CmdPacketCompoundCreature):
		return "ENUM_CMDPACKET_COMPOUND_CREATURE"
	case uint16(CmdPacketCheckCreateGuildAgit):
		return "ENUM_CMDPACKET_CHECK_CREATE_GUILD_AGIT"
	case uint16(CmdPacketCreateGuildAgit):
		return "ENUM_CMDPACKET_CREATE_GUILD_AGIT"
	case uint16(CmdPacketDeleteGuildAgit):
		return "ENUM_CMDPACKET_DELETE_GUILD_AGIT"
	case uint16(CmdPacketPowerWarInfo):
		return "ENUM_CMDPACKET_POWER_WAR_INFO"
	case uint16(CmdPacketUpgradeGuildAgit):
		return "ENUM_CMDPACKET_UPGRADE_GUILD_AGIT"
	case uint16(CmdPacketPowerWarProcessInfo):
		return "ENUM_CMDPACKET_POWER_WAR_PROCESS_INFO"
	case uint16(CmdPacketHackshieldMessageboxBug):
		return "ENUM_CMDPACKET_HACKSHIELD_MESSAGEBOX_BUG"
	case uint16(CmdPacketCreateDisjointStore):
		return "ENUM_CMDPACKET_CREATE_DISJOINT_STORE"
	case uint16(CmdPacketRequestDisjointItem):
		return "ENUM_CMDPACKET_REQUEST_DISJOINT_ITEM"
	case uint16(CmdPacketRepairDisjointMachine):
		return "ENUM_CMDPACKET_REPAIR_DISJOINT_MACHINE"
	case uint16(CmdPacketTeleport):
		return "ENUM_CMDPACKET_TELEPORT"
	case uint16(CmdPacketCompoundItemByExpertJob):
		return "ENUM_CMDPACKET_COMPOUND_ITEM_BY_EXPERT_JOB"
	case uint16(CmdPacketGiveupExpertJob):
		return "ENUM_CMDPACKET_GIVEUP_EXPERT_JOB"
	case uint16(CmdPacketUpgradeDisjointMachine):
		return "ENUM_CMDPACKET_UPGRADE_DISJOINT_MACHINE"
	case uint16(CmdPacketEnterDisjointStore):
		return "ENUM_CMDPACKET_ENTER_DISJOINT_STORE"
	case uint16(CmdPacketCloseDisjointStore):
		return "ENUM_CMDPACKET_CLOSE_DISJOINT_STORE"
	case uint16(CmdPacketReportAbuseUser):
		return "ENUM_CMDPACKET_REPORT_ABUSE_USER"
	case uint16(CmdPacketLoadCompleteAfterAssault):
		return "ENUM_CMDPACKET_LOAD_COMPLETE_AFTER_ASSAULT"
	case uint16(CmdPacketConnectP2pAfterAssault):
		return "ENUM_CMDPACKET_CONNECT_P2P_AFTER_ASSAULT"
	case uint16(CmdPacketChangeNPCFavorDebug):
		return "ENUM_CMDPACKET_CHANGE_NPC_FAVOR_DEBUG"
	case uint16(CmdPacketGuildCargoPushItem):
		return "ENUM_CMDPACKET_GUILD_CARGO_PUSH_ITEM"
	case uint16(CmdPacketGuildCargoPopItem):
		return "ENUM_CMDPACKET_GUILD_CARGO_POP_ITEM"
	case uint16(CmdPacketGuildCargoMoveItem):
		return "ENUM_CMDPACKET_GUILD_CARGO_MOVE_ITEM"
	case uint16(CmdPacketLodingTimeReport):
		return "ENUM_CMDPACKET_LODING_TIME_REPORT"
	case uint16(CmdPacketUseSharedEffectItem):
		return "ENUM_CMDPACKET_USE_SHARED_EFFECT_ITEM"
	case uint16(CmdPacketBuyCerashopLimitItem):
		return "ENUM_CMDPACKET_BUY_CERASHOP_LIMIT_ITEM"
	case uint16(CmdPacketAddHacktypeCnt):
		return "ENUM_CMDPACKET_ADD_HACKTYPE_CNT"
	case uint16(CmdPacketChangeEmotion):
		return "ENUM_CMDPACKET_CHANGE_EMOTION"
	case uint16(CmdPacketDieBloodMonster):
		return "ENUM_CMDPACKET_DIE_BLOOD_MONSTER"
	case uint16(CmdPacketCompoundEmblem):
		return "ENUM_CMDPACKET_COMPOUND_EMBLEM"
	case uint16(CmdPacketMotionResult):
		return "ENUM_CMDPACKET_MOTION_RESULT"
	case uint16(CmdPacketBloodRoundUIPrepareFinish):
		return "ENUM_CMDPACKET_BLOOD_ROUND_UI_PREPARE_FINISH_"
	case uint16(CmdPacketRequestConditionEventReward):
		return "ENUM_CMDPACKET_REQUEST_CONDITION_EVENT_REWARD"
	case uint16(CmdPacketChangeAnotherSkillTree):
		return "ENUM_CMDPACKET_CHANGE_ANOTHER_SKILL_TREE"
	case uint16(CmdPacketGuildCargo):
		return "ENUM_CMDPACKET_GUILD_CARGO"
	case uint16(CmdPacketGuildCargoHistory):
		return "ENUM_CMDPACKET_GUILD_CARGO_HISTORY"
	case uint16(CmdPacketFightVillageMonster):
		return "ENUM_CMDPACKET_FIGHT_VILLAGE_MONSTER"
	case uint16(CmdPacketFinishVillageMonsterFighting):
		return "ENUM_CMDPACKET_FINISH_VILLAGE_MONSTER_FIGHTING"
	case uint16(CmdPacketUpgradeGuildCargo):
		return "ENUM_CMDPACKET_UPGRADE_GUILD_CARGO"
	case uint16(CmdPacketMoveMapReport):
		return "ENUM_CMDPACKET_MOVE_MAP_REPORT"
	case uint16(CmdPacketRequestItemLock):
		return "ENUM_CMDPACKET_REQUEST_ITEM_LOCK"
	case uint16(CmdPacketRequestItemUnlock):
		return "ENUM_CMDPACKET_REQUEST_ITEM_UNLOCK"
	case uint16(CmdPacketRequestItemUnlockCancel):
		return "ENUM_CMDPACKET_REQUEST_ITEM_UNLOCK_CANCEL"
	case uint16(CmdPacketRequestItemUnlockOtp):
		return "ENUM_CMDPACKET_REQUEST_ITEM_UNLOCK_OTP"
	case uint16(CmdPacketUpgradeChronicle):
		return "ENUM_CMDPACKET_UPGRADE_CHRONICLE"
	case uint16(CmdPacketEnchantByBead):
		return "ENUM_CMDPACKET_ENCHANT_BY_BEAD"
	case uint16(CmdPacketDungeonNPCBuffInfo):
		return "ENUM_CMDPACKET_DUNGEON_NPC_BUFF_INFO"
	case uint16(CmdPacketCreateWarroom):
		return "ENUM_CMDPACKET_CREATE_WARROOM"
	case uint16(CmdPacketStartWarroom):
		return "ENUM_CMDPACKET_START_WARROOM"
	case uint16(CmdPacketWarroomChangeTeam):
		return "ENUM_CMDPACKET_WARROOM_CHANGE_TEAM"
	case uint16(CmdPacketWarroomReady):
		return "ENUM_CMDPACKET_WARROOM_READY"
	case uint16(CmdPacketWarroomRandomTeam):
		return "ENUM_CMDPACKET_WARROOM_RANDOM_TEAM"
	case uint16(CmdPacketLagStatistics):
		return "ENUM_CMDPACKET_LAG_STATISTICS"
	case uint16(CmdPacketHtPs):
		return "ENUM_CMDPACKET_HT_PS"
	case uint16(CmdPacketHtIs):
		return "ENUM_CMDPACKET_HT_IS"
	case uint16(CmdPacketHackLevelUp):
		return "ENUM_CMDPACKET_HACK_LEVEL_UP"
	case uint16(CmdPacketPi):
		return "ENUM_CMDPACKET_PI"
	case uint16(CmdPacketVerifyGold):
		return "ENUM_CMDPACKET_VERIFY_GOLD"
	case uint16(CmdPacketOntimeEventRequestReward):
		return "ENUM_CMDPACKET_ONTIME_EVENT_REQUEST_REWARD"
	case uint16(CmdPacketRequestAddPVPBuddy):
		return "ENUM_CMDPACKET_REQUEST_ADD_PVP_BUDDY"
	case uint16(CmdPacketResponseAddPVPBuddy):
		return "ENUM_CMDPACKET_RESPONSE_ADD_PVP_BUDDY"
	case uint16(CmdPacketRemovePVPBuddy):
		return "ENUM_CMDPACKET_REMOVE_PVP_BUDDY"
	case uint16(CmdPacketPVPBuddyConnList):
		return "ENUM_CMDPACKET_PVP_BUDDY_CONN_LIST"
	case uint16(CmdPacketAddUnitedServerFriend):
		return "ENUM_CMDPACKET_ADD_UNITED_SERVER_FRIEND"
	case uint16(CmdPacketDeleteUnitedServerFriend):
		return "ENUM_CMDPACKET_DELETE_UNITED_SERVER_FRIEND"
	case uint16(CmdPacketCheckFinishLoading):
		return "ENUM_CMDPACKET_CHECK_FINISH_LOADING"
	case uint16(CmdPacketNcc):
		return "ENUM_CMDPACKET_NCC"
	case uint16(CmdPacketMi):
		return "ENUM_CMDPACKET_MI"
	case uint16(CmdPacketChangeCharacSlot):
		return "ENUM_CMDPACKET_CHANGE_CHARAC_SLOT"
	case uint16(CmdPacketSecretShopBuyItem):
		return "ENUM_CMDPACKET_SECRET_SHOP_BUY_ITEM"
	case uint16(CmdPacketSecretShopOpenClose):
		return "ENUM_CMDPACKET_SECRET_SHOP_OPEN_CLOSE"
	case uint16(CmdPacketCompleteLoadPVP):
		return "ENUM_CMDPACKET_COMPLETE_LOAD_PVP"
	case uint16(CmdPacketConnectP2pPVP):
		return "ENUM_CMDPACKET_CONNECT_P2P_PVP"
	case uint16(CmdPacketBiddingRoutingItem):
		return "ENUM_CMDPACKET_BIDDING_ROUTING_ITEM"
	case uint16(CmdPacketUseSamsungRandomboxItem):
		return "ENUM_CMDPACKET_USE_SAMSUNG_RANDOMBOX_ITEM"
	case uint16(CmdPacketUseGoblinRandomboxItem):
		return "ENUM_CMDPACKET_USE_GOBLIN_RANDOMBOX_ITEM"
	case uint16(CmdPacketBreakGuild):
		return "ENUM_CMDPACKET_BREAK_GUILD"
	case uint16(CmdPacketRequestQuestAutoClear):
		return "ENUM_CMDPACKET_REQUEST_QUEST_AUTO_CLEAR"
	case uint16(CmdPacketCreateAccountCargo):
		return "ENUM_CMDPACKET_CREATE_ACCOUNT_CARGO"
	case uint16(CmdPacketUpgradeAccountCargo):
		return "ENUM_CMDPACKET_UPGRADE_ACCOUNT_CARGO"
	case uint16(CmdPacketDepositMoney):
		return "ENUM_CMDPACKET_DEPOSIT_MONEY"
	case uint16(CmdPacketWithdrawMoney):
		return "ENUM_CMDPACKET_WITHDRAW_MONEY"
	case uint16(CmdPacketRedeemList):
		return "ENUM_CMDPACKET_REDEEM_LIST"
	case uint16(CmdPacketRedeem):
		return "ENUM_CMDPACKET_REDEEM"
	case uint16(CmdPacketSecuDataControl):
		return "ENUM_CMDPACKET_SECU_DATA_CONTROL"
	case uint16(CmdPacketConnectLinkCharac):
		return "ENUM_CMDPACKET_CONNECT_LINK_CHARAC"
	case uint16(CmdPacketDisconnectLinkCharac):
		return "ENUM_CMDPACKET_DISCONNECT_LINK_CHARAC"
	case uint16(CmdPacketChangeCharacLinkType):
		return "ENUM_CMDPACKET_CHANGE_CHARAC_LINK_TYPE"
	case uint16(CmdPacketMultiMailboxSend):
		return "ENUM_CMDPACKET_MULTI_MAILBOX_SEND"
	case uint16(CmdPacketOperateRidableObject):
		return "ENUM_CMDPACKET_OPERATE_RIDABLE_OBJECT"
	case uint16(CmdPacketSelectUltimateDifficulty):
		return "ENUM_CMDPACKET_SELECT_ULTIMATE_DIFFICULTY"
	case uint16(CmdPacketPiv):
		return "ENUM_CMDPACKET_PIV"
	case uint16(CmdPacketPid):
		return "ENUM_CMDPACKET_PID"
	case uint16(CmdPacketEcoEventItem):
		return "ENUM_CMDPACKET_ECO_EVENT_ITEM"
	case uint16(CmdPacketEnterVipRoom):
		return "ENUM_CMDPACKET_ENTER_VIP_ROOM"
	case uint16(CmdPacketGetDetectiveGoblinItem):
		return "ENUM_CMDPACKET_GET_DETECTIVE_GOBLIN_ITEM"
	case uint16(CmdPacketUseCreatureEvolutionItem):
		return "ENUM_CMDPACKET_USE_CREATURE_EVOLUTION_ITEM"
	case uint16(CmdPacketQueryCharacInfoMailbox):
		return "ENUM_CMDPACKET_QUERY_CHARAC_INFO_MAILBOX"
	case uint16(CmdPacketCompoundItemBindShpere):
		return "ENUM_CMDPACKET_COMPOUND_ITEM_BIND_SHPERE"
	case uint16(CmdPacketUseRidable):
		return "ENUM_CMDPACKET_USE_RIDABLE"
	case uint16(CmdPacketCancelRidable):
		return "ENUM_CMDPACKET_CANCEL_RIDABLE"
	case uint16(CmdPacketChangePartyPosition):
		return "ENUM_CMDPACKET_CHANGE_PARTY_POSITION"
	case uint16(CmdPacketOneToOneChatState):
		return "ENUM_CMDPACKET_ONE_TO_ONE_CHAT_STATE"
	case uint16(CmdPacketFindCharNameUseCharacNo):
		return "ENUM_CMDPACKET_FIND_CHAR_NAME_USE_CHARAC_NO"
	case uint16(CmdPacketSkillCommandCustomizing):
		return "ENUM_CMDPACKET_SKILL_COMMAND_CUSTOMIZING"
	case uint16(CmdPacketSkillCommandAllDefault):
		return "ENUM_CMDPACKET_SKILL_COMMAND_ALL_DEFAULT"
	case uint16(CmdPacketHackScriptHash):
		return "ENUM_CMDPACKET_HACK_SCRIPT_HASH"
	case uint16(CmdPacketAuctionBuyItemApiece):
		return "ENUM_CMDPACKET_AUCTION_BUY_ITEM_APIECE"
	case uint16(CmdPacketChangePartyMemberPosition):
		return "ENUM_CMDPACKET_CHANGE_PARTY_MEMBER_POSITION"
	case uint16(CmdPacketAccff):
		return "ENUM_CMDPACKET_ACCFF"
	case uint16(CmdPacketScanBotByDll):
		return "ENUM_CMDPACKET_SCAN_BOT_BY_DLL"
	case uint16(CmdPacketUseLimitCube):
		return "ENUM_CMDPACKET_USE_LIMIT_CUBE"
	case uint16(CmdPacketRefreshGuildInfo):
		return "ENUM_CMDPACKET_REFRESH_GUILD_INFO"
	case uint16(CmdPacketOpenGuildBoard):
		return "ENUM_CMDPACKET_OPEN_GUILD_BOARD"
	case uint16(CmdPacketWriteOnTheGuildboard):
		return "ENUM_CMDPACKET_WRITE_ON_THE_GUILDBOARD"
	case uint16(CmdPacketDeleteGuildboardText):
		return "ENUM_CMDPACKET_DELETE_GUILDBOARD_TEXT"
	case uint16(CmdPacketRefreshGuildboard):
		return "ENUM_CMDPACKET_REFRESH_GUILDBOARD"
	case uint16(CmdPacketRequestVideoObserver):
		return "ENUM_CMDPACKET_REQUEST_VIDEO_OBSERVER"
	case uint16(CmdPacketStopVideoObserver):
		return "ENUM_CMDPACKET_STOP_VIDEO_OBSERVER"
	case uint16(CmdPacketDonateGuildFund):
		return "ENUM_CMDPACKET_DONATE_GUILD_FUND"
	case uint16(CmdPacketCheckJoinGuild):
		return "ENUM_CMDPACKET_CHECK_JOIN_GUILD"
	case uint16(CmdPacketRequestJoinGuild):
		return "ENUM_CMDPACKET_REQUEST_JOIN_GUILD"
	case uint16(CmdPacketCancelJoinGuild):
		return "ENUM_CMDPACKET_CANCEL_JOIN_GUILD"
	case uint16(CmdPacketApproveJoinGuild):
		return "ENUM_CMDPACKET_APPROVE_JOIN_GUILD"
	case uint16(CmdPacketDenyJoinGuild):
		return "ENUM_CMDPACKET_DENY_JOIN_GUILD"
	case uint16(CmdPacketGuildJoinList):
		return "ENUM_CMDPACKET_GUILD_JOIN_LIST"
	case uint16(CmdPacketRequestVideoObserverError):
		return "ENUM_CMDPACKET_REQUEST_VIDEO_OBSERVER_ERROR"
	case uint16(CmdPacketResponseVideoObserver):
		return "ENUM_CMDPACKET_RESPONSE_VIDEO_OBSERVER"
	case uint16(CmdPacketGuildAttendanceInfo):
		return "ENUM_CMDPACKET_GUILD_ATTENDANCE_INFO"
	case uint16(CmdPacketPassGateObject):
		return "ENUM_CMDPACKET_PASS_GATE_OBJECT"
	case uint16(CmdPacketDungeonMotion):
		return "ENUM_CMDPACKET_DUNGEON_MOTION"
	case uint16(CmdPacketRequestOverseer):
		return "ENUM_CMDPACKET_REQUEST_OVERSEER"
	case uint16(CmdPacketInsertOverseer):
		return "ENUM_CMDPACKET_INSERT_OVERSEER"
	case uint16(CmdPacketCompoundEquipmentUpgradeCard):
		return "ENUM_CMDPACKET_COMPOUND_EQUIPMENT_UPGRADE_CARD"
	case uint16(CmdPacketRefundCeraItem):
		return "ENUM_CMDPACKET_REFUND_CERA_ITEM"
	case uint16(CmdPacketPickupCeraItem):
		return "ENUM_CMDPACKET_PICKUP_CERA_ITEM"
	case uint16(CmdPacketCashInventory):
		return "ENUM_CMDPACKET_CASH_INVENTORY"
	case uint16(CmdPacketBreakAwayQuestCheck):
		return "ENUM_CMDPACKET_BREAK_AWAY_QUEST_CHECK"
	case uint16(CmdPacketJoinGuildInfo):
		return "ENUM_CMDPACKET_JOIN_GUILD_INFO"
	case uint16(CmdPacketScanBotByDrv):
		return "ENUM_CMDPACKET_SCAN_BOT_BY_DRV"
	case uint16(CmdPacketAskRematch):
		return "ENUM_CMDPACKET_ASK_REMATCH"
	case uint16(CmdPacketSaveGameOptionQuickchatting):
		return "ENUM_CMDPACKET_SAVE_GAME_OPTION_QUICKCHATTING"
	case uint16(CmdPacketSelect3rdChronicleItemForEnchant):
		return "ENUM_CMDPACKET_SELECT_3RD_CHRONICLE_ITEM_FOR_ENCHANT"
	case uint16(CmdPacketEnchant3rdChronicleItem):
		return "ENUM_CMDPACKET_ENCHANT_3RD_CHRONICLE_ITEM"
	case uint16(CmdPacketGoldTakeIncreasingAmount):
		return "ENUM_CMDPACKET_GOLD_TAKE_INCREASING_AMOUNT"
	case uint16(CmdPacketUseHackByOtherPartyMemberUid):
		return "ENUM_CMDPACKET_USE_HACK_BY_OTHER_PARTY_MEMBER_UID"
	case uint16(CmdPacketCheckSecurityProtection):
		return "ENUM_CMDPACKET_CHECK_SECURITY_PROTECTION"
	case uint16(CmdPacketIntegrateMatchPVPScore):
		return "ENUM_CMDPACKET_INTEGRATE_MATCH_PVP_SCORE"
	case uint16(CmdPacketFairPVPScore):
		return "ENUM_CMDPACKET_FAIR_PVP_SCORE"
	case uint16(CmdPacketPVPMissionHpPercent):
		return "ENUM_CMDPACKET_PVP_MISSION_HP_PERCENT"
	case uint16(CmdPacketPVPMissionWinPose):
		return "ENUM_CMDPACKET_PVP_MISSION_WIN_POSE"
	case uint16(CmdPacketWarroomWpPerMonster):
		return "ENUM_CMDPACKET_WARROOM_WP_PER_MONSTER"
	case uint16(CmdPacketSetLabyrinthReadyState):
		return "ENUM_CMDPACKET_SET_LABYRINTH_READY_STATE"
	case uint16(CmdPacketSetLabyrinthSeatState):
		return "ENUM_CMDPACKET_SET_LABYRINTH_SEAT_STATE"
	case uint16(CmdPacketRequestLabyrinthMonsterUid):
		return "ENUM_CMDPACKET_REQUEST_LABYRINTH_MONSTER_UID"
	case uint16(CmdPacketFinishLoadingLabyrinth):
		return "ENUM_CMDPACKET_FINISH_LOADING_LABYRINTH"
	case uint16(CmdPacketDieLabyrinthMonster):
		return "ENUM_CMDPACKET_DIE_LABYRINTH_MONSTER"
	case uint16(CmdPacketDestroyLabyrinthObject):
		return "ENUM_CMDPACKET_DESTROY_LABYRINTH_OBJECT"
	case uint16(CmdPacketDieLabyrinthCharacter):
		return "ENUM_CMDPACKET_DIE_LABYRINTH_CHARACTER"
	case uint16(CmdPacketDestroyLabyrinthCenterObject):
		return "ENUM_CMDPACKET_DESTROY_LABYRINTH_CENTER_OBJECT"
	case uint16(CmdPacketRequestDungeonPartyList):
		return "ENUM_CMDPACKET_REQUEST_DUNGEON_PARTY_LIST"
	case uint16(CmdPacketRegisterCargoPad):
		return "ENUM_CMDPACKET_REGISTER_CARGO_PAD"
	case uint16(CmdPacketModifyCargoPad):
		return "ENUM_CMDPACKET_MODIFY_CARGO_PAD"
	case uint16(CmdPacketUnregisterCargoPad):
		return "ENUM_CMDPACKET_UNREGISTER_CARGO_PAD"
	case uint16(CmdPacketCancelCargoPad):
		return "ENUM_CMDPACKET_CANCEL_CARGO_PAD"
	case uint16(CmdPacketCargoPadStatus):
		return "ENUM_CMDPACKET_CARGO_PAD_STATUS"
	case uint16(CmdPacketCargopadKeyReq):
		return "ENUM_CMDPACKET_CARGOPAD_KEY_REQ"
	case uint16(CmdPacketCargopadCertify):
		return "ENUM_CMDPACKET_CARGOPAD_CERTIFY"
	case uint16(CmdPacketRequestVideoObserverLog):
		return "ENUM_CMDPACKET_REQUEST_VIDEO_OBSERVER_LOG"
	case uint16(CmdPacketAbnormalFunctionCall):
		return "ENUM_CMDPACKET_ABNORMAL_FUNCTION_CALL"
	case uint16(CmdPacketEquipslotSwitch):
		return "ENUM_CMDPACKET_EQUIPSLOT_SWITCH"
	case uint16(CmdPacketExpandEquipslotFlagUpdate):
		return "ENUM_CMDPACKET_EXPAND_EQUIPSLOT_FLAG_UPDATE"
	case uint16(CmdPacketBuyCharacStatusUsingQp):
		return "ENUM_CMDPACKET_BUY_CHARAC_STATUS_USING_QP"
	case uint16(CmdPacketClearUsedQp):
		return "ENUM_CMDPACKET_CLEAR_USED_QP"
	case uint16(CmdPacketUnsealRandomOption):
		return "ENUM_CMDPACKET_UNSEAL_RANDOM_OPTION"
	case uint16(CmdPacketMobileReqAuthNo):
		return "ENUM_CMDPACKET_MOBILE_REQ_AUTH_NO"
	case uint16(CmdPacketMobileReqVerifyAuthNo):
		return "ENUM_CMDPACKET_MOBILE_REQ_VERIFY_AUTH_NO"
	case uint16(CmdPacketChangeHostWarroom):
		return "ENUM_CMDPACKET_CHANGE_HOST_WARROOM"
	case uint16(CmdPacketVerifyPrivateStoreItem):
		return "ENUM_CMDPACKET_VERIFY_PRIVATE_STORE_ITEM"
	case uint16(CmdPacketSelectItem):
		return "ENUM_CMDPACKET_SELECT_ITEM"
	case uint16(CmdPacketRegenerationRandomOption):
		return "ENUM_CMDPACKET_REGENERATION_RANDOM_OPTION"
	case uint16(CmdPacketUpgradeCargo):
		return "ENUM_CMDPACKET_UPGRADE_CARGO"
	case uint16(CmdPacketSelectIdolPot):
		return "ENUM_CMDPACKET_SELECT_IDOL_POT"
	case uint16(CmdPacketIdolBringUp):
		return "ENUM_CMDPACKET_IDOL_BRING_UP"
	case uint16(CmdPacketPVPMissionCombo):
		return "ENUM_CMDPACKET_PVP_MISSION_COMBO"
	case uint16(CmdPacketTitleBookPut):
		return "ENUM_CMDPACKET_TITLE_BOOK_PUT"
	case uint16(CmdPacketTitleBookGet):
		return "ENUM_CMDPACKET_TITLE_BOOK_GET"
	case uint16(CmdPacketMonstercardBind):
		return "ENUM_CMDPACKET_MONSTERCARD_BIND"
	case uint16(CmdPacketCharacSlotExtendEffect):
		return "ENUM_CMDPACKET_CHARAC_SLOT_EXTEND_EFFECT"
	case uint16(CmdPacketExpertExtraction):
		return "ENUM_CMDPACKET_EXPERT_EXTRACTION"
	case uint16(CmdPacketAchievementTrigger):
		return "ENUM_CMDPACKET_ACHIEVEMENT_TRIGGER"
	case uint16(CmdPacketRequestEventServerLevelUp):
		return "ENUM_CMDPACKET_REQUEST_EVENT_SERVER_LEVEL_UP"
	case uint16(CmdPacketInviteMemberForGroup):
		return "ENUM_CMDPACKET_INVITE_MEMBER_FOR_GROUP"
	case uint16(CmdPacketLeaveFromGroup):
		return "ENUM_CMDPACKET_LEAVE_FROM_GROUP"
	case uint16(CmdPacketChangeGroupChatUserState):
		return "ENUM_CMDPACKET_CHANGE_GROUP_CHAT_USER_STATE"
	case uint16(CmdPacketEventAllUserGift):
		return "ENUM_CMDPACKET_EVENT_ALL_USER_GIFT"
	case uint16(CmdPacketOtherUserTitleBookList):
		return "ENUM_CMDPACKET_OTHER_USER_TITLE_BOOK_LIST"
	case uint16(CmdPacketItemHyperlinkMessage):
		return "ENUM_CMDPACKET_ITEM_HYPERLINK_MESSAGE"
	case uint16(CmdPacketUserHistoryLog):
		return "ENUM_CMDPACKET_USER_HISTORY_LOG"
	case uint16(CmdPacketRefundSkill):
		return "ENUM_CMDPACKET_REFUND_SKILL"
	case uint16(CmdPacketRegisterPlayer):
		return "ENUM_CMDPACKET_REGISTER_PLAYER"
	case uint16(CmdPacketAttendanceCheck):
		return "ENUM_CMDPACKET_ATTENDANCE_CHECK"
	case uint16(CmdPacketRequestGoblinPadImg):
		return "ENUM_CMDPACKET_REQUEST_GOBLIN_PAD_IMG"
	case uint16(CmdPacketUpgradeInventory):
		return "ENUM_CMDPACKET_UPGRADE_INVENTORY"
	case uint16(CmdPacketSelectItemGrowthPower):
		return "ENUM_CMDPACKET_SELECT_ITEM_GROWTH_POWER"
	case uint16(CmdPacketRequestSeriaBuff):
		return "ENUM_CMDPACKET_REQUEST_SERIA_BUFF"
	case uint16(CmdPacketUseChestnutStoneItem):
		return "ENUM_CMDPACKET_USE_CHESTNUT_STONE_ITEM"
	case uint16(CmdPacketPartyTeleport):
		return "ENUM_CMDPACKET_PARTY_TELEPORT"
	case uint16(CmdPacketPartyTeleportConfirm):
		return "ENUM_CMDPACKET_PARTY_TELEPORT_CONFIRM"
	case uint16(CmdPacketAbnormalUseStackable):
		return "ENUM_CMDPACKET_ABNORMAL_USE_STACKABLE"
	case uint16(CmdPacketChangeRandomOption):
		return "ENUM_CMDPACKET_CHANGE_RANDOM_OPTION"
	case uint16(CmdPacketUpgradeItemSeparate):
		return "ENUM_CMDPACKET_UPGRADE_ITEM_SEPARATE"
	case uint16(CmdPacketItemDictionary):
		return "ENUM_CMDPACKET_ITEM_DICTIONARY"
	case uint16(CmdPacketMercenaryReturn):
		return "ENUM_CMDPACKET_MERCENARY_RETURN"
	case uint16(CmdPacketMercenaryInfo):
		return "ENUM_CMDPACKET_MERCENARY_INFO"
	case uint16(CmdPacketMercenaryCompetition):
		return "ENUM_CMDPACKET_MERCENARY_COMPETITION"
	case uint16(CmdPacketRegisterQuickParty):
		return "ENUM_CMDPACKET_REGISTER_QUICK_PARTY"
	case uint16(CmdPacketCancelQuickParty):
		return "ENUM_CMDPACKET_CANCEL_QUICK_PARTY"
	case uint16(CmdPacketDirectEntranceDungeonQuickParty):
		return "ENUM_CMDPACKET_DIRECT_ENTRANCE_DUNGEON_QUICK_PARTY"
	case uint16(CmdPacketRequestAssaultPrice):
		return "ENUM_CMDPACKET_REQUEST_ASSAULT_PRICE"
	case uint16(CmdPacketSaveCharacterOption):
		return "ENUM_CMDPACKET_SAVE_CHARACTER_OPTION"
	case uint16(CmdPacketExchangeRandomItemReward):
		return "ENUM_CMDPACKET_EXCHANGE_RANDOM_ITEM_REWARD"
	case uint16(CmdPacketAvatarDisjointRandomReward):
		return "ENUM_CMDPACKET_AVATAR_DISJOINT_RANDOM_REWARD"
	case uint16(CmdPacketCheck3rdpartyConcent):
		return "ENUM_CMDPACKET_CHECK_3RDPARTY_CONCENT"
	case uint16(CmdPacketLoggingCryptedType):
		return "ENUM_CMDPACKET_LOGGING_CRYPTED_TYPE"
	case uint16(CmdPacketFloatRdataModulation):
		return "ENUM_CMDPACKET_FLOAT_RDATA_MODULATION"
	case uint16(CmdPacketReqUrgentQuest):
		return "ENUM_CMDPACKET_REQ_URGENT_QUEST"
	case uint16(CmdPacketInsertRandomOptionItem):
		return "ENUM_CMDPACKET_INSERT_RANDOM_OPTION_ITEM"
	case uint16(CmdPacketResetRandomOption):
		return "ENUM_CMDPACKET_RESET_RANDOM_OPTION"
	case uint16(CmdPacketClearQuest):
		return "ENUM_CMDPACKET_CLEAR_QUEST"
	case uint16(CmdPacketTournamentRewardSelectState):
		return "ENUM_CMDPACKET_TOURNAMENT_REWARD_SELECT_STATE"
	case uint16(CmdPacketTournamentRewardSelect):
		return "ENUM_CMDPACKET_TOURNAMENT_REWARD_SELECT"
	case uint16(CmdPacketAvatarOptionChange):
		return "ENUM_CMDPACKET_AVATAR_OPTION_CHANGE"
	case uint16(CmdPacketCharacterStatus):
		return "ENUM_CMDPACKET_CHARACTER_STATUS"
	case uint16(CmdPacketRequestSocialEventCoinItem):
		return "ENUM_CMDPACKET_REQUEST_SOCIAL_EVENT_COIN_ITEM"
	case uint16(CmdPacketRequestSocialEventMember):
		return "ENUM_CMDPACKET_REQUEST_SOCIAL_EVENT_MEMBER"
	case uint16(CmdPacketResponseSocialEventMember):
		return "ENUM_CMDPACKET_RESPONSE_SOCIAL_EVENT_MEMBER"
	case uint16(CmdPacketLimitNPCBuyItem):
		return "ENUM_CMDPACKET_LIMIT_NPC_BUY_ITEM"
	case uint16(CmdPacketQueryCharacInfoAddData):
		return "ENUM_CMDPACKET_QUERY_CHARAC_INFO_ADD_DATA"
	case uint16(CmdPacket3MonthStopStatistic):
		return "ENUM_CMDPACKET_3_MONTH_STOP_STATISTIC"
	case uint16(CmdPacketSeriaRoomDecoInfo):
		return "ENUM_CMDPACKET_SERIA_ROOM_DECO_INFO"
	case uint16(CmdPacketObjectBringUpUseItem):
		return "ENUM_CMDPACKET_OBJECT_BRING_UP_USE_ITEM"
	case uint16(CmdPacketPrecheckSoloTelepoart):
		return "ENUM_CMDPACKET_PRECHECK_SOLO_TELEPOART"
	case uint16(CmdPacketSoloTelepoart):
		return "ENUM_CMDPACKET_SOLO_TELEPOART"
	case uint16(CmdPacket2012NeweventPutitem):
		return "ENUM_CMDPACKET_2012_NEWEVENT_PUTITEM"
	case uint16(CmdPacketSaveGameOptionChattingEmoticon):
		return "ENUM_CMDPACKET_SAVE_GAME_OPTION_CHATTING_EMOTICON"
	case uint16(CmdPacketReportClientHack):
		return "ENUM_CMDPACKET_REPORT_CLIENT_HACK"
	case uint16(CmdPacketRdataSectionModulation):
		return "ENUM_CMDPACKET_RDATA_SECTION_MODULATION"
	case uint16(CmdPacketImageCommunicationEquipmentUse):
		return "ENUM_CMDPACKET_IMAGE_COMMUNICATION_EQUIPMENT_USE"
	case uint16(CmdPacketCompatibilityIndex):
		return "ENUM_CMDPACKET_COMPATIBILITY_INDEX"
	case uint16(CmdPacketInformNotice):
		return "ENUM_CMDPACKET_INFORM_NOTICE"
	case uint16(CmdPacketP2pStatistics):
		return "ENUM_CMDPACKET_P2P_STATISTICS"
	case uint16(CmdPacketVerifyCreatureQuest):
		return "ENUM_CMDPACKET_VERIFY_CREATURE_QUEST"
	case uint16(CmdPacketReportPVPLag):
		return "ENUM_CMDPACKET_REPORT_PVP_LAG"
	case uint16(CmdPacketVerifyPVPLagUser):
		return "ENUM_CMDPACKET_VERIFY_PVP_LAG_USER"
	case uint16(CmdPacketCollectItems):
		return "ENUM_CMDPACKET_COLLECT_ITEMS"
	case uint16(CmdPacketTutorialLevelUp):
		return "ENUM_CMDPACKET_TUTORIAL_LEVEL_UP"
	case uint16(CmdPacketRequestCharacSkillInfo):
		return "ENUM_CMDPACKET_REQUEST_CHARAC_SKILL_INFO"
	case uint16(CmdPacketCraneStartUse):
		return "ENUM_CMDPACKET_CRANE_START_USE"
	case uint16(CmdPacketCranePickup):
		return "ENUM_CMDPACKET_CRANE_PICKUP"
	case uint16(CmdPacketSelectStriker):
		return "ENUM_CMDPACKET_SELECT_STRIKER"
	case uint16(CmdPacketRequestIngameAdvertisement):
		return "ENUM_CMDPACKET_REQUEST_INGAME_ADVERTISEMENT"
	case uint16(CmdPacketLogIngameAdvertisement):
		return "ENUM_CMDPACKET_LOG_INGAME_ADVERTISEMENT"
	case uint16(CmdPacketAutoSkill):
		return "ENUM_CMDPACKET_AUTO_SKILL"
	case uint16(CmdPacketSkillInit):
		return "ENUM_CMDPACKET_SKILL_INIT"
	case uint16(CmdPacketPCRoomPlayTimeReward):
		return "ENUM_CMDPACKET_PC_ROOM_PLAY_TIME_REWARD"
	case uint16(CmdPacketPCRoomRentItem):
		return "ENUM_CMDPACKET_PC_ROOM_RENT_ITEM"
	case uint16(CmdPacketSeriaroomDecoEvent):
		return "ENUM_CMDPACKET_SERIAROOM_DECO_EVENT"
	case uint16(CmdPacketBlueMarble):
		return "ENUM_CMDPACKET_BLUE_MARBLE"
	case uint16(CmdPacketGetGrowthcapsule):
		return "ENUM_CMDPACKET_GET_GROWTHCAPSULE"
	case uint16(CmdPacketDynamicScriptReloading):
		return "ENUM_CMDPACKET_DYNAMIC_SCRIPT_RELOADING"
	case uint16(CmdPacketUseDye):
		return "ENUM_CMDPACKET_USE_DYE"
	case uint16(CmdPacketPVPHistoryLog):
		return "ENUM_CMDPACKET_PVP_HISTORY_LOG"
	case uint16(CmdPacketPVPUseSkill):
		return "ENUM_CMDPACKET_PVP_USE_SKILL"
	case uint16(CmdPacketOnTimeEventWhileOneHourGift):
		return "ENUM_CMDPACKET_ON_TIME_EVENT_WHILE_ONE_HOUR_GIFT"
	case uint16(CmdPacketUseRightOfChangeGrowType):
		return "ENUM_CMDPACKET_USE_RIGHT_OF_CHANGE_GROW_TYPE"
	case uint16(CmdPacketInformNotice2nd):
		return "ENUM_CMDPACKET_INFORM_NOTICE_2ND"
	case uint16(CmdPacketGrowthWeaponChangeInfinity):
		return "ENUM_CMDPACKET_GROWTH_WEAPON_CHANGE_INFINITY"
	case uint16(CmdPacketGrowthWeaponUseMaterial):
		return "ENUM_CMDPACKET_GROWTH_WEAPON_USE_MATERIAL"
	case uint16(CmdPacketSaveQuestNotify):
		return "ENUM_CMDPACKET_SAVE_QUEST_NOTIFY"
	case uint16(CmdPacketBlueMarbleConfirmInfo):
		return "ENUM_CMDPACKET_BLUE_MARBLE_CONFIRM_INFO"
	case uint16(CmdPacketComboSkillInfo):
		return "ENUM_CMDPACKET_COMBO_SKILL_INFO"
	case uint16(CmdPacketUseRenameCard):
		return "ENUM_CMDPACKET_USE_RENAME_CARD"
	case uint16(CmdPacketComboSkillExtensionQuickSlotReset):
		return "ENUM_CMDPACKET_COMBO_SKILL_EXTENSION_QUICK_SLOT_RESET"
	case uint16(CmdPacketEquipedCreatureChangeInfinityCreature):
		return "ENUM_CMDPACKET_EQUIPED_CREATURE_CHANGE_INFINITY_CREATURE"
	case uint16(CmdPacketSeriaroomAnimationDecoEvent):
		return "ENUM_CMDPACKET_SERIAROOM_ANIMATION_DECO_EVENT"
	case uint16(CmdPacketBingoReward):
		return "ENUM_CMDPACKET_BINGO_REWARD"
	case uint16(CmdPacketBingoQuiz):
		return "ENUM_CMDPACKET_BINGO_QUIZ"
	case uint16(CmdPacketUseStackableAction):
		return "ENUM_CMDPACKET_USE_STACKABLE_ACTION"
	case uint16(CmdPacketDualRaidDungeonJoin):
		return "ENUM_CMDPACKET_DUAL_RAID_DUNGEON_JOIN"
	case uint16(CmdPacketDualRaidDungeon):
		return "ENUM_CMDPACKET_DUAL_RAID_DUNGEON"
	case uint16(CmdPacketOpenCerapackage):
		return "ENUM_CMDPACKET_OPEN_CERAPACKAGE"
	case uint16(CmdPacketGetItembox):
		return "ENUM_CMDPACKET_GET_ITEMBOX"
	case uint16(CmdPacketRequestIntegratedMatching):
		return "ENUM_CMDPACKET_REQUEST_INTEGRATED_MATCHING"
	case uint16(CmdPacketCancelIntegratedMatching):
		return "ENUM_CMDPACKET_CANCEL_INTEGRATED_MATCHING"
	case uint16(CmdPacketMatchingDungeonExit):
		return "ENUM_CMDPACKET_MATCHING_DUNGEON_EXIT"
	case uint16(CmdPacketChannelMoveSuccess):
		return "ENUM_CMDPACKET_CHANNEL_MOVE_SUCCESS"
	case uint16(CmdPacketRequestEventRanking):
		return "ENUM_CMDPACKET_REQUEST_EVENT_RANKING"
	case uint16(CmdPacketAddEventRanking):
		return "ENUM_CMDPACKET_ADD_EVENT_RANKING"
	case uint16(CmdPacketRequestColosseumPurchaseTicket):
		return "ENUM_CMDPACKET_REQUEST_COLOSSEUM_PURCHASE_TICKET"
	case uint16(CmdPacketRequestUpdateColosseumReward):
		return "ENUM_CMDPACKET_REQUEST_UPDATE_COLOSSEUM_REWARD"
	case uint16(CmdPacketSummonMonster):
		return "ENUM_CMDPACKET_SUMMON_MONSTER"
	case uint16(CmdPacketRacingDungeonJoin):
		return "ENUM_CMDPACKET_RACING_DUNGEON_JOIN"
	case uint16(CmdPacketRacingDungeonDisjoin):
		return "ENUM_CMDPACKET_RACING_DUNGEON_DISJOIN"
	case uint16(CmdPacketRacingDungeonGoalInPlayer):
		return "ENUM_CMDPACKET_RACING_DUNGEON_GOAL_IN_PLAYER"
	case uint16(CmdPacketRacingDungeonReturnToVillage):
		return "ENUM_CMDPACKET_RACING_DUNGEON_RETURN_TO_VILLAGE"
	case uint16(CmdPacketDualRaidDungeonReady):
		return "ENUM_CMDPACKET_DUAL_RAID_DUNGEON_READY"
	case uint16(CmdPacketDualRaidDungeonReward):
		return "ENUM_CMDPACKET_DUAL_RAID_DUNGEON_REWARD"
	case uint16(CmdPacketUpdateContractOfCubeInfo):
		return "ENUM_CMDPACKET_UPDATE_CONTRACT_OF_CUBE_INFO"
	case uint16(CmdPacketRequestFreelyGiveItemBox):
		return "ENUM_CMDPACKET_REQUEST_FREELY_GIVE_ITEM_BOX"
	case uint16(CmdPacketRequestCouponChange):
		return "ENUM_CMDPACKET_REQUEST_COUPON_CHANGE"
	case uint16(CmdPacketUseFreeGiveItemCoupon):
		return "ENUM_CMDPACKET_USE_FREE_GIVE_ITEM_COUPON"
	case uint16(CmdPacketChangePeriodicToUnlimitItem):
		return "ENUM_CMDPACKET_CHANGE_PERIODIC_TO_UNLIMIT_ITEM"
	case uint16(CmdPacketInitFreeGiveItemEvent):
		return "ENUM_CMDPACKET_INIT_FREE_GIVE_ITEM_EVENT"
	case uint16(CmdPacketSelectZombie):
		return "ENUM_CMDPACKET_SELECT_ZOMBIE"
	case uint16(CmdPacketToBeZombie):
		return "ENUM_CMDPACKET_TO_BE_ZOMBIE"
	case uint16(CmdPacketZombieModeResultScore):
		return "ENUM_CMDPACKET_ZOMBIE_MODE_RESULT_SCORE"
	case uint16(CmdPacketDecideTimeStepAttendance):
		return "ENUM_CMDPACKET_DECIDE_TIME_STEP_ATTENDANCE"
	case uint16(CmdPacketRequestForParticipation):
		return "ENUM_CMDPACKET_REQUEST_FOR_PARTICIPATION"
	case uint16(CmdPacketMonsterMoveSystem):
		return "ENUM_CMDPACKET_MONSTER_MOVE_SYSTEM"
	case uint16(CmdPacketKiriCargoPurchase):
		return "ENUM_CMDPACKET_KIRI_CARGO_PURCHASE"
	case uint16(CmdPacketKiriCargoGetBonus):
		return "ENUM_CMDPACKET_KIRI_CARGO_GET_BONUS"
	case uint16(CmdPacketPVPTournamentMatchList):
		return "ENUM_CMDPACKET_PVP_TOURNAMENT_MATCH_LIST"
	case uint16(CmdPacketPVPTournamentRequest):
		return "ENUM_CMDPACKET_PVP_TOURNAMENT_REQUEST"
	case uint16(CmdPacketLoadingCart):
		return "ENUM_CMDPACKET_LOADING_CART"
	case uint16(CmdPacketGoldenCommerceStart):
		return "ENUM_CMDPACKET_GOLDEN_COMMERCE_START"
	case uint16(CmdPacketGoldenCommerceReward):
		return "ENUM_CMDPACKET_GOLDEN_COMMERCE_REWARD"
	case uint16(CmdPacketCreatureFillpoint):
		return "ENUM_CMDPACKET_CREATURE_FILLPOINT"
	case uint16(CmdPacketChurnGradeActionClear):
		return "ENUM_CMDPACKET_CHURN_GRADE_ACTION_CLEAR"
	case uint16(CmdPacketCargoTransportItem):
		return "ENUM_CMDPACKET_CARGO_TRANSPORT_ITEM"
	case uint16(CmdPacketDungeonMissionStart):
		return "ENUM_CMDPACKET_DUNGEON_MISSION_START"
	case uint16(CmdPacketDungeonMissionUpdate):
		return "ENUM_CMDPACKET_DUNGEON_MISSION_UPDATE"
	case uint16(CmdPacketDungeonMissionCheckSuccess):
		return "ENUM_CMDPACKET_DUNGEON_MISSION_CHECK_SUCCESS"
	case uint16(CmdPacketRequestInstantReinforce):
		return "ENUM_CMDPACKET_REQUEST_INSTANT_REINFORCE"
	case uint16(CmdPacketFatigueAccelerationStateChange):
		return "ENUM_CMDPACKET_FATIGUE_ACCELERATION_STATE_CHANGE"
	case uint16(CmdPacketMinorityDetailResult):
		return "ENUM_CMDPACKET_MINORITY_DETAIL_RESULT"
	case uint16(CmdPacketMinorityRequestQuestion):
		return "ENUM_CMDPACKET_MINORITY_REQUEST_QUESTION"
	case uint16(CmdPacketMinorityQuestionAnswer):
		return "ENUM_CMDPACKET_MINORITY_QUESTION_ANSWER"
	case uint16(CmdPacketMinorityRequireReward):
		return "ENUM_CMDPACKET_MINORITY_REQUIRE_REWARD"
	case uint16(CmdPacketDecideLevelup):
		return "ENUM_CMDPACKET_DECIDE_LEVELUP"
	case uint16(CmdPacketSetCloneTitle):
		return "ENUM_CMDPACKET_SET_CLONE_TITLE"
	case uint16(CmdPacketSurveyContents):
		return "ENUM_CMDPACKET_SURVEY_CONTENTS"
	case uint16(CmdPacketIntegrateMatchingDirectComplete):
		return "ENUM_CMDPACKET_INTEGRATE_MATCHING_DIRECT_COMPLETE"
	case uint16(CmdPacketIntegratedMatchingModeChange):
		return "ENUM_CMDPACKET_INTEGRATED_MATCHING_MODE_CHANGE"
	case uint16(CmdPacketIntegrateMatchingUserCount):
		return "ENUM_CMDPACKET_INTEGRATE_MATCHING_USER_COUNT"
	case uint16(CmdPacketCollectPrivateData):
		return "ENUM_CMDPACKET_COLLECT_PRIVATE_DATA"
	case uint16(CmdPacketCollectPrivateSerialData):
		return "ENUM_CMDPACKET_COLLECT_PRIVATE_SERIAL_DATA"
	case uint16(CmdPacketCollectPrivateNoReplay):
		return "ENUM_CMDPACKET_COLLECT_PRIVATE_NO_REPLAY"
	case uint16(CmdPacketCreateUpgradeRoom):
		return "ENUM_CMDPACKET_CREATE_UPGRADE_ROOM"
	case uint16(CmdPacketDestroyUpgradeRoom):
		return "ENUM_CMDPACKET_DESTROY_UPGRADE_ROOM"
	case uint16(CmdPacketJoinSpectatorUpgradeRoom):
		return "ENUM_CMDPACKET_JOIN_SPECTATOR_UPGRADE_ROOM"
	case uint16(CmdPacketLeaveSpectatorUpgradeRoom):
		return "ENUM_CMDPACKET_LEAVE_SPECTATOR_UPGRADE_ROOM"
	case uint16(CmdPacketUpgradeRoomGiveItemInfo):
		return "ENUM_CMDPACKET_UPGRADE_ROOM_GIVE_ITEM_INFO"
	case uint16(CmdPacketModuleExist):
		return "ENUM_CMDPACKET_MODULE_EXIST"
	case uint16(CmdPacketModuleRequest):
		return "ENUM_CMDPACKET_MODULE_REQUEST"
	case uint16(CmdPacketRequestComboScore):
		return "ENUM_CMDPACKET_REQUEST_COMBO_SCORE"
	case uint16(CmdPacketComboScoreInfo):
		return "ENUM_CMDPACKET_COMBO_SCORE_INFO"
	case uint16(CmdPacketAddRainbowPoint):
		return "ENUM_CMDPACKET_ADD_RAINBOW_POINT"
	case uint16(CmdPacketRequestRainbowPoint):
		return "ENUM_CMDPACKET_REQUEST_RAINBOW_POINT"
	case uint16(CmdPacketRequestRainbowPointReward):
		return "ENUM_CMDPACKET_REQUEST_RAINBOW_POINT_REWARD"
	case uint16(CmdPacketUpgradeRoomPutUpItem):
		return "ENUM_CMDPACKET_UPGRADE_ROOM_PUT_UP_ITEM"
	case uint16(CmdPacketUpgradeRoomUpgradeStart):
		return "ENUM_CMDPACKET_UPGRADE_ROOM_UPGRADE_START"
	case uint16(CmdPacketUpgradeRoomUpgradeCancel):
		return "ENUM_CMDPACKET_UPGRADE_ROOM_UPGRADE_CANCEL"
	case uint16(CmdPacketUpgradeRoomMasterMessage):
		return "ENUM_CMDPACKET_UPGRADE_ROOM_MASTER_MESSAGE"
	case uint16(CmdPacketCharmProlong):
		return "ENUM_CMDPACKET_CHARM_PROLONG"
	case uint16(CmdPacketSecurityStatus):
		return "ENUM_CMDPACKET_SECURITY_STATUS"
	case uint16(CmdPacketEventNPCDropItem):
		return "ENUM_CMDPACKET_EVENT_NPC_DROP_ITEM_"
	case uint16(CmdPacketRequestPickEventInfo):
		return "ENUM_CMDPACKET_REQUEST_PICK_EVENT_INFO"
	case uint16(CmdPacketLetsPickPresent):
		return "ENUM_CMDPACKET_LETS_PICK_PRESENT"
	case uint16(CmdPacketRequestAddPickChance):
		return "ENUM_CMDPACKET_REQUEST_ADD_PICK_CHANCE"
	case uint16(CmdPacketCreateExpertJobStore):
		return "ENUM_CMDPACKET_CREATE_EXPERT_JOB_STORE"
	case uint16(CmdPacketEnterExpertJobStore):
		return "ENUM_CMDPACKET_ENTER_EXPERT_JOB_STORE"
	case uint16(CmdPacketCloseExpertJobStore):
		return "ENUM_CMDPACKET_CLOSE_EXPERT_JOB_STORE"
	case uint16(CmdPacketUseEnchantStore):
		return "ENUM_CMDPACKET_USE_ENCHANT_STORE"
	case uint16(CmdPacketGetExpandExpGageReward):
		return "ENUM_CMDPACKET_GET_EXPAND_EXP_GAGE_REWARD"
	case uint16(CmdPacketUpgradeCard):
		return "ENUM_CMDPACKET_UPGRADE_CARD"
	case uint16(CmdPacketUserRankCombo):
		return "ENUM_CMDPACKET_USER_RANK_COMBO"
	case uint16(CmdPacketUseObjectScaleEffectInVillage):
		return "ENUM_CMDPACKET_USE_OBJECT_SCALE_EFFECT_IN_VILLAGE"
	case uint16(CmdPacketCancleObjectScaleEffectInVillage):
		return "ENUM_CMDPACKET_CANCLE_OBJECT_SCALE_EFFECT_IN_VILLAGE"
	case uint16(CmdPacketRequestChildrenDayEventReward):
		return "ENUM_CMDPACKET_REQUEST_CHILDREN_DAY_EVENT_REWARD"
	case uint16(CmdPacketRequestExpKeepingEventReward):
		return "ENUM_CMDPACKET_REQUEST_EXP_KEEPING_EVENT_REWARD"
	case uint16(CmdPacketRequestSpTeam):
		return "ENUM_CMDPACKET_REQUEST_SP_TEAM"
	case uint16(CmdPacketStrongestPVPPrivateInfo):
		return "ENUM_CMDPACKET_STRONGEST_PVP_PRIVATE_INFO"
	case uint16(CmdPacketRepairExpertJobStore):
		return "ENUM_CMDPACKET_REPAIR_EXPERT_JOB_STORE"
	case uint16(CmdPacketAppendageDestoryObject):
		return "ENUM_CMDPACKET_APPENDAGE_DESTORY_OBJECT"
	case uint16(CmdPacketStrongestPVPMemberChannelinfo):
		return "ENUM_CMDPACKET_STRONGEST_PVP_MEMBER_CHANNELINFO"
	case uint16(CmdPacketUpdateEventDungeonTopRank):
		return "ENUM_CMDPACKET_UPDATE_EVENT_DUNGEON_TOP_RANK"
	case uint16(CmdPacketTeraPieceClearTimeRankRequest):
		return "ENUM_CMDPACKET_TERA_PIECE_CLEAR_TIME_RANK_REQUEST"
	case uint16(CmdPacketBroadcastPVPMakeItem):
		return "ENUM_CMDPACKET_BROADCAST_PVP_MAKE_ITEM"
	case uint16(CmdPacketBroadcastPVPSelectedCharacter):
		return "ENUM_CMDPACKET_BROADCAST_PVP_SELECTED_CHARACTER"
	case uint16(CmdPacketTimerModifyInfo):
		return "ENUM_CMDPACKET_TIMER_MODIFY_INFO"
	case uint16(CmdPacketSummonTimeLine):
		return "ENUM_CMDPACKET_SUMMON_TIME_LINE"
	case uint16(CmdPacketSeaChaseMiniGameResult):
		return "ENUM_CMDPACKET_SEA_CHASE_MINI_GAME_RESULT"
	case uint16(CmdPacketRequestUpdateSpecEvent):
		return "ENUM_CMDPACKET_REQUEST_UPDATE_SPEC_EVENT"
	case uint16(CmdPacketRequestSpecEventReward):
		return "ENUM_CMDPACKET_REQUEST_SPEC_EVENT_REWARD"
	case uint16(CmdPacketGainEatObject):
		return "ENUM_CMDPACKET_GAIN_EAT_OBJECT"
	case uint16(CmdPacketPVPDailyWinCount):
		return "ENUM_CMDPACKET_PVP_DAILY_WIN_COUNT"
	case uint16(CmdPacketDeletePVPFightDollTicket):
		return "ENUM_CMDPACKET_DELETE_PVP_FIGHT_DOLL_TICKET"
	case uint16(CmdPacketSelectFightDoll):
		return "ENUM_CMDPACKET_SELECT_FIGHT_DOLL"
	case uint16(CmdPacketUsePVPDollSkill):
		return "ENUM_CMDPACKET_USE_PVP_DOLL_SKILL"
	case uint16(CmdPacketFightDollRankIdx):
		return "ENUM_CMDPACKET_FIGHT_DOLL_RANK_IDX"
	case uint16(CmdPacketAttendanceMarbleEventInfo):
		return "ENUM_CMDPACKET_ATTENDANCE_MARBLE_EVENT_INFO"
	case uint16(CmdPacketAttendanceMarbleEventDice):
		return "ENUM_CMDPACKET_ATTENDANCE_MARBLE_EVENT_DICE"
	case uint16(CmdPacketUserStateMotion):
		return "ENUM_CMDPACKET_USER_STATE_MOTION"
	case uint16(CmdPacketGetPcroomTimePointItem):
		return "ENUM_CMDPACKET_GET_PCROOM_TIME_POINT_ITEM"
	case uint16(CmdPacketGetAvatarSpecEvent):
		return "ENUM_CMDPACKET_GET_AVATAR_SPEC_EVENT"
	case uint16(CmdPacketRequestEventDungeonTopRankInfo):
		return "ENUM_CMDPACKET_REQUEST_EVENT_DUNGEON_TOP_RANK_INFO"
	case uint16(CmdPacketDungeonClear):
		return "ENUM_CMDPACKET_DUNGEON_CLEAR"
	case uint16(CmdPacketDuringLoginTimeEventAction):
		return "ENUM_CMDPACKET_DURING_LOGIN_TIME_EVENT_ACTION"
	case uint16(CmdPacketBidLegendaryAuction):
		return "ENUM_CMDPACKET_BID_LEGENDARY_AUCTION"
	case uint16(CmdPacketCharacterCreateCountPerDayForKor):
		return "ENUM_CMDPACKET_CHARACTER_CREATE_COUNT_PER_DAY_FOR_KOR"
	case uint16(CmdPacketReturnUserRewardRequest):
		return "ENUM_CMDPACKET_RETURN_USER_REWARD_REQUEST"
	case uint16(CmdPacketRequestFatigueUseReward):
		return "ENUM_CMDPACKET_REQUEST_FATIGUE_USE_REWARD"
	case uint16(CmdPacketAttendanceMarbleDoubleExit):
		return "ENUM_CMDPACKET_ATTENDANCE_MARBLE_DOUBLE_EXIT"
	case uint16(CmdPacketSetExpectPartyList):
		return "ENUM_CMDPACKET_SET_EXPECT_PARTY_LIST"
	case uint16(CmdPacketPVPRoomForSimulation):
		return "ENUM_CMDPACKET_PVP_ROOM_FOR_SIMULATION"
	case uint16(CmdPacketOtpCheck):
		return "ENUM_CMDPACKET_OTP_CHECK"
	case uint16(CmdPacketCharacViewHiddenCharacInfo):
		return "ENUM_CMDPACKET_CHARAC_VIEW_HIDDEN_CHARAC_INFO"
	case uint16(CmdPacketRebirthHardcoreCharac):
		return "ENUM_CMDPACKET_REBIRTH_HARDCORE_CHARAC"
	case uint16(CmdPacketHardcoreCharacList):
		return "ENUM_CMDPACKET_HARDCORE_CHARAC_LIST"
	case uint16(CmdPacketHardcoreMurderer):
		return "ENUM_CMDPACKET_HARDCORE_MURDERER"
	case uint16(CmdPacketRequestResetHardcoreCharac):
		return "ENUM_CMDPACKET_REQUEST_RESET_HARDCORE_CHARAC"
	case uint16(CmdPacketHardcoreRank):
		return "ENUM_CMDPACKET_HARDCORE_RANK"
	case uint16(CmdPacketRequestEventGift):
		return "ENUM_CMDPACKET_REQUEST_EVENT_GIFT"
	case uint16(CmdPacketAssaultReviveUseMoney):
		return "ENUM_CMDPACKET_ASSAULT_REVIVE_USE_MONEY"
	case uint16(CmdPacketDimensionExperienceUserReward):
		return "ENUM_CMDPACKET_DIMENSION_EXPERIENCE_USER_REWARD"
	case uint16(CmdPacketDimensionExperienceMentor):
		return "ENUM_CMDPACKET_DIMENSION_EXPERIENCE_MENTOR"
	case uint16(CmdPacketDimensionExperienceClearReward):
		return "ENUM_CMDPACKET_DIMENSION_EXPERIENCE_CLEAR_REWARD"
	case uint16(CmdPacketUsingSkillLog):
		return "ENUM_CMDPACKET_USING_SKILL_LOG"
	case uint16(CmdPacketChangeDeckInfo):
		return "ENUM_CMDPACKET_CHANGE_DECK_INFO"
	case uint16(CmdPacketRaidEntryCostInfo):
		return "ENUM_CMDPACKET_RAID_ENTRY_COST_INFO"
	case uint16(CmdPacketRaidMovieSkip):
		return "ENUM_CMDPACKET_RAID_MOVIE_SKIP"
	case uint16(CmdPacketSelectRaidRewardCard):
		return "ENUM_CMDPACKET_SELECT_RAID_REWARD_CARD"
	case uint16(CmdPacketRaidDoBehavior):
		return "ENUM_CMDPACKET_RAID_DO_BEHAVIOR"
	case uint16(CmdPacketRaidSetSymbol):
		return "ENUM_CMDPACKET_RAID_SET_SYMBOL"
	case uint16(CmdPacketRaidMessage):
		return "ENUM_CMDPACKET_RAID_MESSAGE"
	case uint16(CmdPacketCreateRaid):
		return "ENUM_CMDPACKET_CREATE_RAID"
	case uint16(CmdPacketLeaveRaid):
		return "ENUM_CMDPACKET_LEAVE_RAID"
	case uint16(CmdPacketStartRaid):
		return "ENUM_CMDPACKET_START_RAID"
	case uint16(CmdPacketSetRaidWaiting):
		return "ENUM_CMDPACKET_SET_RAID_WAITING"
	case uint16(CmdPacketRejoinRaid):
		return "ENUM_CMDPACKET_REJOIN_RAID"
	case uint16(CmdPacketRaidManagerWork):
		return "ENUM_CMDPACKET_RAID_MANAGER_WORK"
	case uint16(CmdPacketModifyRaidInfo):
		return "ENUM_CMDPACKET_MODIFY_RAID_INFO"
	case uint16(CmdPacketDisjointColosseumItem):
		return "ENUM_CMDPACKET_DISJOINT_COLOSSEUM_ITEM"
	case uint16(CmdPacketRequestStarMarketingCreature):
		return "ENUM_CMDPACKET_REQUEST_STAR_MARKETING_CREATURE"
	case uint16(CmdPacketChangeStarMarketingInfiniteCreature):
		return "ENUM_CMDPACKET_CHANGE_STAR_MARKETING_INFINITE_CREATURE"
	case uint16(CmdPacketRecentFriendList):
		return "ENUM_CMDPACKET_RECENT_FRIEND_LIST"
	case uint16(CmdPacketPVPSeasonReward):
		return "ENUM_CMDPACKET_PVP_SEASON_REWARD"
	case uint16(CmdPacketGunner2AwakeningUserReward):
		return "ENUM_CMDPACKET_GUNNER_2_AWAKENING_USER_REWARD"
	case uint16(CmdPacketGunner2AwakeningClearReward):
		return "ENUM_CMDPACKET_GUNNER_2_AWAKENING_CLEAR_REWARD"
	case uint16(CmdPacketRequestItemGrowtypeExperience):
		return "ENUM_CMDPACKET_REQUEST_ITEM_GROWTYPE_EXPERIENCE"
	case uint16(CmdPacketLoadExtendCharacs):
		return "ENUM_CMDPACKET_LOAD_EXTEND_CHARACS"
	case uint16(CmdPacketStartSlotMachine):
		return "ENUM_CMDPACKET_START_SLOT_MACHINE"
	case uint16(CmdPacketExecuteSlotMachine):
		return "ENUM_CMDPACKET_EXECUTE_SLOT_MACHINE"
	case uint16(CmdPacketBingoReset):
		return "ENUM_CMDPACKET_BINGO_RESET"
	case uint16(CmdPacketReqHumanCertify):
		return "ENUM_CMDPACKET_REQ_HUMAN_CERTIFY"
	case uint16(CmdPacketRecoveryDeleteCharacter):
		return "ENUM_CMDPACKET_RECOVERY_DELETE_CHARACTER"
	case uint16(CmdPacketGetCouponSystemReward):
		return "ENUM_CMDPACKET_GET_COUPON_SYSTEM_REWARD"
	case uint16(CmdPacketLevelUpEventReward):
		return "ENUM_CMDPACKET_LEVEL_UP_EVENT_REWARD"
	case uint16(CmdPacketIllusionUpgradeState):
		return "ENUM_CMDPACKET_ILLUSION_UPGRADE_STATE"
	case uint16(CmdPacketEventReward):
		return "ENUM_CMDPACKET_EVENT_REWARD"
	case uint16(CmdPacketEventRequest):
		return "ENUM_CMDPACKET_EVENT_REQUEST"
	case uint16(CmdPacketStaticsRuntimeTing):
		return "ENUM_CMDPACKET_STATICS_RUNTIME_TING"
	case uint16(CmdPacketReserveLeaveParty):
		return "ENUM_CMDPACKET_RESERVE_LEAVE_PARTY"
	case uint16(CmdPacketCheckDoubleCharacterName):
		return "ENUM_CMDPACKET_CHECK_DOUBLE_CHARACTER_NAME"
	case uint16(CmdPacketCrackOfDimension):
		return "ENUM_CMDPACKET_CRACK_OF_DIMENSION"
	case uint16(CmdPacketRaidBuffSystem):
		return "ENUM_CMDPACKET_RAID_BUFF_SYSTEM"
	case uint16(CmdPacketRaidMonsterHp):
		return "ENUM_CMDPACKET_RAID_MONSTER_HP"
	case uint16(CmdPacketDungeonBonusMonster):
		return "ENUM_CMDPACKET_DUNGEON_BONUS_MONSTER"
	case uint16(CmdPacketPcroomAttendanceEventCheck):
		return "ENUM_CMDPACKET_PCROOM_ATTENDANCE_EVENT_CHECK"
	case uint16(CmdPacketUserAttendanceEventCheck):
		return "ENUM_CMDPACKET_USER_ATTENDANCE_EVENT_CHECK"
	case uint16(CmdPacketDailyChallengeReward):
		return "ENUM_CMDPACKET_DAILY_CHALLENGE_REWARD"
	case uint16(CmdPacketRequestStudyJoinReward):
		return "ENUM_CMDPACKET_REQUEST_STUDY_JOIN_REWARD"
	case uint16(CmdPacketFarmEventAction):
		return "ENUM_CMDPACKET_FARM_EVENT_ACTION"
	case uint16(CmdPacketUpdateSpecEvent):
		return "ENUM_CMDPACKET_UPDATE_SPEC_EVENT"
	case uint16(CmdPacketGetPcroomWithFriendItem):
		return "ENUM_CMDPACKET_GET_PCROOM_WITH_FRIEND_ITEM"
	case uint16(CmdPacketContentsPlayInfo):
		return "ENUM_CMDPACKET_CONTENTS_PLAY_INFO"
	case uint16(CmdPacketEntryIntoParty):
		return "ENUM_CMDPACKET_ENTRY_INTO_PARTY"
	case uint16(CmdPacketEntryIntoPartyFinish):
		return "ENUM_CMDPACKET_ENTRY_INTO_PARTY_FINISH"
	case uint16(CmdPacketEquipmentSwapInfo):
		return "ENUM_CMDPACKET_EQUIPMENT_SWAP_INFO"
	case uint16(CmdPacketJoinRealEstate):
		return "ENUM_CMDPACKET_JOIN_REAL_ESTATE"
	case uint16(CmdPacketLeaveRealEstate):
		return "ENUM_CMDPACKET_LEAVE_REAL_ESTATE"
	case uint16(CmdPacketStartRealEstate):
		return "ENUM_CMDPACKET_START_REAL_ESTATE"
	case uint16(CmdPacketRealEstateUserState):
		return "ENUM_CMDPACKET_REAL_ESTATE_USER_STATE"
	case uint16(CmdPacketUDPPacketStatistic):
		return "ENUM_CMDPACKET_UDP_PACKET_STATISTIC"
	case uint16(CmdPacketRequestItemSortLock):
		return "ENUM_CMDPACKET_REQUEST_ITEM_SORT_LOCK"
	case uint16(CmdPacketRequestItemSortUnlock):
		return "ENUM_CMDPACKET_REQUEST_ITEM_SORT_UNLOCK"
	case uint16(CmdPacketShopPurchaseCount):
		return "ENUM_CMDPACKET_SHOP_PURCHASE_COUNT"
	case uint16(CmdPacketSlotMachineUpdateItemList):
		return "ENUM_CMDPACKET_SLOT_MACHINE_UPDATE_ITEM_LIST"
	case uint16(CmdPacketSetPcroomChoiceAndFocusMission):
		return "ENUM_CMDPACKET_SET_PCROOM_CHOICE_AND_FOCUS_MISSION"
	case uint16(CmdPacketCancelChoiceAndFocusMission):
		return "ENUM_CMDPACKET_CANCEL_CHOICE_AND_FOCUS_MISSION"
	case uint16(CmdPacketGetPcroomChoiceAndFocusItem):
		return "ENUM_CMDPACKET_GET_PCROOM_CHOICE_AND_FOCUS_ITEM"
	case uint16(CmdPacketTerritoryCombatSymbol):
		return "ENUM_CMDPACKET_TERRITORY_COMBAT_SYMBOL"
	case uint16(CmdPacketTerritoryCombatRespawn):
		return "ENUM_CMDPACKET_TERRITORY_COMBAT_RESPAWN"
	case uint16(CmdPacketTerritoryCombatSituationRoom):
		return "ENUM_CMDPACKET_TERRITORY_COMBAT_SITUATION_ROOM"
	case uint16(CmdPacketTerritoryCombatSkill):
		return "ENUM_CMDPACKET_TERRITORY_COMBAT_SKILL"
	case uint16(CmdPacketTerritoryCombatHp):
		return "ENUM_CMDPACKET_TERRITORY_COMBAT_HP"
	case uint16(CmdPacketTerritoryCombatSupplyItem):
		return "ENUM_CMDPACKET_TERRITORY_COMBAT_SUPPLY_ITEM"
	case uint16(CmdPacketTerritoryCombatRequestSupplyItem):
		return "ENUM_CMDPACKET_TERRITORY_COMBAT_REQUEST_SUPPLY_ITEM"
	case uint16(CmdPacketTerritoryCombatChangeRole):
		return "ENUM_CMDPACKET_TERRITORY_COMBAT_CHANGE_ROLE"
	case uint16(CmdPacketTerritoryCombatSecretPath):
		return "ENUM_CMDPACKET_TERRITORY_COMBAT_SECRET_PATH"
	case uint16(CmdPacketTerritoryCombatReqMoveDungeon):
		return "ENUM_CMDPACKET_TERRITORY_COMBAT_REQ_MOVE_DUNGEON"
	case uint16(CmdPacketTerritoryCombatResMoveDungeon):
		return "ENUM_CMDPACKET_TERRITORY_COMBAT_RES_MOVE_DUNGEON"
	case uint16(CmdPacketTerritoryCombatWaitRoom):
		return "ENUM_CMDPACKET_TERRITORY_COMBAT_WAIT_ROOM"
	case uint16(CmdPacketTerritoryCombatDungeonResult):
		return "ENUM_CMDPACKET_TERRITORY_COMBAT_DUNGEON_RESULT"
	case uint16(CmdPacketRentalItemEventInfo):
		return "ENUM_CMDPACKET_RENTAL_ITEM_EVENT_INFO"
	case uint16(CmdPacketDungeonDamageInfo):
		return "ENUM_CMDPACKET_DUNGEON_DAMAGE_INFO"
	case uint16(CmdPacketAnalysisDungeonDamageInfo):
		return "ENUM_CMDPACKET_ANALYSIS_DUNGEON_DAMAGE_INFO"
	case uint16(CmdPacketUseGoldcardRealEstate):
		return "ENUM_CMDPACKET_USE_GOLDCARD_REAL_ESTATE"
	case uint16(CmdPacketRejoinDungeon):
		return "ENUM_CMDPACKET_REJOIN_DUNGEON"
	case uint16(CmdPacketCancelRejoinDungeon):
		return "ENUM_CMDPACKET_CANCEL_REJOIN_DUNGEON"
	case uint16(CmdPacketCheckGuildCreatePromoteMsg):
		return "ENUM_CMDPACKET_CHECK_GUILD_CREATE_PROMOTE_MSG"
	case uint16(CmdPacketModifyGuildPromoteMsg):
		return "ENUM_CMDPACKET_MODIFY_GUILD_PROMOTE_MSG"
	case uint16(CmdPacketRequestGuildCreatePermit):
		return "ENUM_CMDPACKET_REQUEST_GUILD_CREATE_PERMIT"
	case uint16(CmdPacketReplyGuildCreatePermit):
		return "ENUM_CMDPACKET_REPLY_GUILD_CREATE_PERMIT"
	case uint16(CmdPacketCancelGuildCreate):
		return "ENUM_CMDPACKET_CANCEL_GUILD_CREATE"
	case uint16(CmdPacketReqGuildInfoOfMyChars):
		return "ENUM_CMDPACKET_REQ_GUILD_INFO_OF_MY_CHARS"
	case uint16(CmdPacketReqGuildSerchForJoin):
		return "ENUM_CMDPACKET_REQ_GUILD_SERCH_FOR_JOIN"
	case uint16(CmdPacketTodayGuildAttendanceDetailInfo):
		return "ENUM_CMDPACKET_TODAY_GUILD_ATTENDANCE_DETAIL_INFO"
	case uint16(CmdPacketReqGuildMileageHistory):
		return "ENUM_CMDPACKET_REQ_GUILD_MILEAGE_HISTORY"
	case uint16(CmdPacketChangeGuildGrade):
		return "ENUM_CMDPACKET_CHANGE_GUILD_GRADE"
	case uint16(CmdPacketChangeGuildGradeName):
		return "ENUM_CMDPACKET_CHANGE_GUILD_GRADE_NAME"
	case uint16(CmdPacketVariableNeedMaterial):
		return "ENUM_CMDPACKET_VARIABLE_NEED_MATERIAL"
	case uint16(CmdPacketTryPuzzle):
		return "ENUM_CMDPACKET_TRY_PUZZLE"
	case uint16(CmdPacketFpsPerformanceStatistic):
		return "ENUM_CMDPACKET_FPS_PERFORMANCE_STATISTIC"
	case uint16(CmdPacketSetUserAreaPreCheck):
		return "ENUM_CMDPACKET_SET_USER_AREA_PRE_CHECK"
	case uint16(CmdPacketReqGuildAlliance):
		return "ENUM_CMDPACKET_REQ_GUILD_ALLIANCE"
	case uint16(CmdPacketReqGuildAllianceList):
		return "ENUM_CMDPACKET_REQ_GUILD_ALLIANCE_LIST"
	case uint16(CmdPacketCancelReqGuildAlliance):
		return "ENUM_CMDPACKET_CANCEL_REQ_GUILD_ALLIANCE"
	case uint16(CmdPacketCallGuildAllianceList):
		return "ENUM_CMDPACKET_CALL_GUILD_ALLIANCE_LIST"
	case uint16(CmdPacketApproveGuildAlliance):
		return "ENUM_CMDPACKET_APPROVE_GUILD_ALLIANCE"
	case uint16(CmdPacketDenyGuildAlliance):
		return "ENUM_CMDPACKET_DENY_GUILD_ALLIANCE"
	case uint16(CmdPacketSecedeGuildAlliance):
		return "ENUM_CMDPACKET_SECEDE_GUILD_ALLIANCE"
	case uint16(CmdPacketBuyGuildContents):
		return "ENUM_CMDPACKET_BUY_GUILD_CONTENTS"
	case uint16(CmdPacketReqRecommendGuild):
		return "ENUM_CMDPACKET_REQ_RECOMMEND_GUILD"
	case uint16(CmdPacketSendGuildPresent):
		return "ENUM_CMDPACKET_SEND_GUILD_PRESENT"
	case uint16(CmdPacketReqGuildPresentsList):
		return "ENUM_CMDPACKET_REQ_GUILD_PRESENTS_LIST"
	case uint16(CmdPacketReqGuildPresentsHistory):
		return "ENUM_CMDPACKET_REQ_GUILD_PRESENTS_HISTORY"
	case uint16(CmdPacketRecvGuildPresent):
		return "ENUM_CMDPACKET_RECV_GUILD_PRESENT"
	case uint16(CmdPacketRegisterGuildSupportCharac):
		return "ENUM_CMDPACKET_REGISTER_GUILD_SUPPORT_CHARAC"
	case uint16(CmdPacketRemoveGuildSupportCharac):
		return "ENUM_CMDPACKET_REMOVE_GUILD_SUPPORT_CHARAC"
	case uint16(CmdPacketRentGuildSupportCharac):
		return "ENUM_CMDPACKET_RENT_GUILD_SUPPORT_CHARAC"
	case uint16(CmdPacketRentCancelGuildSupportCharac):
		return "ENUM_CMDPACKET_RENT_CANCEL_GUILD_SUPPORT_CHARAC"
	case uint16(CmdPacketInfoGuildSupportCharac):
		return "ENUM_CMDPACKET_INFO_GUILD_SUPPORT_CHARAC"
	case uint16(CmdPacketUseGuildSupportCharac):
		return "ENUM_CMDPACKET_USE_GUILD_SUPPORT_CHARAC"
	case uint16(CmdPacketWebViewerInfo):
		return "ENUM_CMDPACKET_WEB_VIEWER_INFO"
	case uint16(CmdPacketRequestSceneStreamReplay):
		return "ENUM_CMDPACKET_REQUEST_SCENE_STREAM_REPLAY"
	case uint16(CmdPacketResponseSceneStreamReplay):
		return "ENUM_CMDPACKET_RESPONSE_SCENE_STREAM_REPLAY"
	case uint16(CmdPacketDestroyAssaultObject):
		return "ENUM_CMDPACKET_DESTROY_ASSAULT_OBJECT"
	case uint16(CmdPacketRequestCircleEnter):
		return "ENUM_CMDPACKET_REQUEST_CIRCLE_ENTER"
	case uint16(CmdPacketCheckTerritoryCombatDeclaration):
		return "ENUM_CMDPACKET_CHECK_TERRITORY_COMBAT_DECLARATION"
	case uint16(CmdPacketReqTerritoryCombatDeclaration):
		return "ENUM_CMDPACKET_REQ_TERRITORY_COMBAT_DECLARATION"
	case uint16(CmdPacketReqTerritoryCombatDeclarationList):
		return "ENUM_CMDPACKET_REQ_TERRITORY_COMBAT_DECLARATION_LIST"
	case uint16(CmdPacketCheckTerritoryCombatIndirectPart):
		return "ENUM_CMDPACKET_CHECK_TERRITORY_COMBAT_INDIRECT_PART"
	case uint16(CmdPacketReqTerritoryCombatIndirectPart):
		return "ENUM_CMDPACKET_REQ_TERRITORY_COMBAT_INDIRECT_PART"
	case uint16(CmdPacketReqTerritoryCombatIndirectPartStatus):
		return "ENUM_CMDPACKET_REQ_TERRITORY_COMBAT_INDIRECT_PART_STATUS"
	case uint16(CmdPacketReqTerritoryCombatIndirectPartReward):
		return "ENUM_CMDPACKET_REQ_TERRITORY_COMBAT_INDIRECT_PART_REWARD"
	case uint16(CmdPacketReGrowupChange):
		return "ENUM_CMDPACKET_RE_GROWUP_CHANGE"
	case uint16(CmdPacketSkillQuickSlotSort):
		return "ENUM_CMDPACKET_SKILL_QUICK_SLOT_SORT"
	case uint16(CmdPacketChangeGuildMark):
		return "ENUM_CMDPACKET_CHANGE_GUILD_MARK"
	case uint16(CmdPacketEpicBookMakeItem):
		return "ENUM_CMDPACKET_EPIC_BOOK_MAKE_ITEM"
	case uint16(CmdPacketRequestServerCharacterList):
		return "ENUM_CMDPACKET_REQUEST_SERVER_CHARACTER_LIST"
	case uint16(CmdPacketScenarioModeClearQuest):
		return "ENUM_CMDPACKET_SCENARIO_MODE_CLEAR_QUEST"
	case uint16(CmdPacketInputUserCongratulatoryTelegram):
		return "ENUM_CMDPACKET_INPUT_USER_CONGRATULATORY_TELEGRAM"
	case uint16(CmdPacketAskForTerritoryCombat):
		return "ENUM_CMDPACKET_ASK_FOR_TERRITORY_COMBAT"
	case uint16(CmdPacketReqTerritoryCombatList):
		return "ENUM_CMDPACKET_REQ_TERRITORY_COMBAT_LIST"
	case uint16(CmdPacketDecideTerritoryCombatList):
		return "ENUM_CMDPACKET_DECIDE_TERRITORY_COMBAT_LIST"
	case uint16(CmdPacketReqDecideTerritoryCombatList):
		return "ENUM_CMDPACKET_REQ_DECIDE_TERRITORY_COMBAT_LIST"
	case uint16(CmdPacketCheckTerritoryCombatChannelEnter):
		return "ENUM_CMDPACKET_CHECK_TERRITORY_COMBAT_CHANNEL_ENTER"
	case uint16(CmdPacketCheckTerritoryCombatExerciseModeTime):
		return "ENUM_CMDPACKET_CHECK_TERRITORY_COMBAT_EXERCISE_MODE_TIME"
	case uint16(CmdPacketAvatarContestInfo):
		return "ENUM_CMDPACKET_AVATAR_CONTEST_INFO"
	case uint16(CmdPacketAvatarContestSubmit):
		return "ENUM_CMDPACKET_AVATAR_CONTEST_SUBMIT"
	case uint16(CmdPacketReqTerritoryOperationFundDistribution):
		return "ENUM_CMDPACKET_REQ_TERRITORY_OPERATION_FUND_DISTRIBUTION"
	case uint16(CmdPacketAvatarConversion):
		return "ENUM_CMDPACKET_AVATAR_CONVERSION"
	case uint16(CmdPacketRequestObjectGrowth):
		return "ENUM_CMDPACKET_REQUEST_OBJECT_GROWTH"
	case uint16(CmdPacketBetFatigue):
		return "ENUM_CMDPACKET_BET_FATIGUE"
	case uint16(CmdPacketRequestUserChannel):
		return "ENUM_CMDPACKET_REQUEST_USER_CHANNEL"
	case uint16(CmdPacketRemakeJumpingCharacter):
		return "ENUM_CMDPACKET_REMAKE_JUMPING_CHARACTER"
	case uint16(CmdPacketIncubatingSystemReward):
		return "ENUM_CMDPACKET_INCUBATING_SYSTEM_REWARD"
	case uint16(CmdPacketIncubatingSystemSetGrowtype):
		return "ENUM_CMDPACKET_INCUBATING_SYSTEM_SET_GROWTYPE"
	case uint16(CmdPacketRequestWarroomReward):
		return "ENUM_CMDPACKET_REQUEST_WARROOM_REWARD"
	case uint16(CmdPacketCommonStructSerializeSample):
		return "ENUM_CMDPACKET_COMMON_STRUCT_SERIALIZE_SAMPLE"
	case uint16(CmdPacketGuildContributeHistory):
		return "ENUM_CMDPACKET_GUILD_CONTRIBUTE_HISTORY"
	case uint16(CmdPacketReqRepresentCharacter):
		return "ENUM_CMDPACKET_REQ_REPRESENT_CHARACTER"
	case uint16(CmdPacketRequestNPCFavorOperation):
		return "ENUM_CMDPACKET_REQUEST_NPC_FAVOR_OPERATION"
	case uint16(CmdPacketGiveNPCBuff):
		return "ENUM_CMDPACKET_GIVE_NPC_BUFF"
	case uint16(CmdPacketLicenseDungeonPlayResult):
		return "ENUM_CMDPACKET_LICENSE_DUNGEON_PLAY_RESULT"
	case uint16(CmdPacketLicenseDungeonRequestReward):
		return "ENUM_CMDPACKET_LICENSE_DUNGEON_REQUEST_REWARD"
	case uint16(CmdPacketEpicPieceExchangeChoice):
		return "ENUM_CMDPACKET_EPIC_PIECE_EXCHANGE_CHOICE"
	case uint16(CmdPacketEpicPieceExchangeMaterialChoice):
		return "ENUM_CMDPACKET_EPIC_PIECE_EXCHANGE_MATERIAL_CHOICE"
	case uint16(CmdPacketEpicPieceExchangeComplete):
		return "ENUM_CMDPACKET_EPIC_PIECE_EXCHANGE_COMPLETE"
	case uint16(CmdPacketRaidOtherChannelUserinfo):
		return "ENUM_CMDPACKET_RAID_OTHER_CHANNEL_USERINFO"
	case uint16(CmdPacketRaidOtherChannelRequestJoin):
		return "ENUM_CMDPACKET_RAID_OTHER_CHANNEL_REQUEST_JOIN"
	case uint16(CmdPacketRaidOtherChannelResponseJoin):
		return "ENUM_CMDPACKET_RAID_OTHER_CHANNEL_RESPONSE_JOIN"
	case uint16(CmdPacketRaidRecentFriendList):
		return "ENUM_CMDPACKET_RAID_RECENT_FRIEND_LIST"
	case uint16(CmdPacketRaidMemberChangeState):
		return "ENUM_CMDPACKET_RAID_MEMBER_CHANGE_STATE"
	case uint16(CmdPacketRaidUserMoveChannelFail):
		return "ENUM_CMDPACKET_RAID_USER_MOVE_CHANNEL_FAIL"
	case uint16(CmdPacketReqTerritoryCombatAllianceList):
		return "ENUM_CMDPACKET_REQ_TERRITORY_COMBAT_ALLIANCE_LIST"
	case uint16(CmdPacketReqTerritoryCombatPointList):
		return "ENUM_CMDPACKET_REQ_TERRITORY_COMBAT_POINT_LIST"
	case uint16(CmdPacketSetTerritoryOperationFundDistributeType):
		return "ENUM_CMDPACKET_SET_TERRITORY_OPERATION_FUND_DISTRIBUTE_TYPE"
	case uint16(CmdPacketMoveMapObject):
		return "ENUM_CMDPACKET_MOVE_MAP_OBJECT"
	case uint16(CmdPacketUseGem):
		return "ENUM_CMDPACKET_USE_GEM"
	case uint16(CmdPacketSetGuildRecommandChannel):
		return "ENUM_CMDPACKET_SET_GUILD_RECOMMAND_CHANNEL"
	case uint16(CmdPacketRaidOtherChannelList):
		return "ENUM_CMDPACKET_RAID_OTHER_CHANNEL_LIST"
	case uint16(CmdPacketSetGuildFlagAura):
		return "ENUM_CMDPACKET_SET_GUILD_FLAG_AURA"
	case uint16(CmdPacketLockDisplayOtp):
		return "ENUM_CMDPACKET_LOCK_DISPLAY_OTP"
	case uint16(CmdPacketUnlockDisplayOtp):
		return "ENUM_CMDPACKET_UNLOCK_DISPLAY_OTP"
	case uint16(CmdPacketCompoundGem):
		return "ENUM_CMDPACKET_COMPOUND_GEM"
	case uint16(CmdPacketCollectUserinfoBinary):
		return "ENUM_CMDPACKET_COLLECT_USERINFO_BINARY"
	case uint16(CmdPacketAgitWarConstructBuilding):
		return "ENUM_CMDPACKET_AGIT_WAR_CONSTRUCT_BUILDING"
	case uint16(CmdPacketAgitWarDestructBuilding):
		return "ENUM_CMDPACKET_AGIT_WAR_DESTRUCT_BUILDING"
	case uint16(CmdPacketAgitWarMoveBuilding):
		return "ENUM_CMDPACKET_AGIT_WAR_MOVE_BUILDING"
	case uint16(CmdPacketAgitWarUpgradeBuilding):
		return "ENUM_CMDPACKET_AGIT_WAR_UPGRADE_BUILDING"
	case uint16(CmdPacketAgitWarMatching):
		return "ENUM_CMDPACKET_AGIT_WAR_MATCHING"
	case uint16(CmdPacketAgitWarMatchingCancel):
		return "ENUM_CMDPACKET_AGIT_WAR_MATCHING_CANCEL"
	case uint16(CmdPacketAgitwarWinpointReward):
		return "ENUM_CMDPACKET_AGITWAR_WINPOINT_REWARD"
	case uint16(CmdPacketReplayZoneGiftCube):
		return "ENUM_CMDPACKET_REPLAY_ZONE_GIFT_CUBE"
	case uint16(CmdPacketReplayZoneClickReplay):
		return "ENUM_CMDPACKET_REPLAY_ZONE_CLICK_REPLAY"
	case uint16(CmdPacketReplayZoneFavoriteUpdate):
		return "ENUM_CMDPACKET_REPLAY_ZONE_FAVORITE_UPDATE"
	case uint16(CmdPacketReplayZoneFavoriteList):
		return "ENUM_CMDPACKET_REPLAY_ZONE_FAVORITE_LIST"
	case uint16(CmdPacketReplayZoneReqUserInfo):
		return "ENUM_CMDPACKET_REPLAY_ZONE_REQ_USER_INFO"
	case uint16(CmdPacketReplayZoneSelfDelete):
		return "ENUM_CMDPACKET_REPLAY_ZONE_SELF_DELETE"
	case uint16(CmdPacketReplayZoneReqBasicInfo):
		return "ENUM_CMDPACKET_REPLAY_ZONE_REQ_BASIC_INFO"
	case uint16(CmdPacketMainHudInfo):
		return "ENUM_CMDPACKET_MAIN_HUD_INFO"
	case uint16(CmdPacketReqBingoMark):
		return "ENUM_CMDPACKET_REQ_BINGO_MARK"
	case uint16(CmdPacketAccountAchievementTrigger):
		return "ENUM_CMDPACKET_ACCOUNT_ACHIEVEMENT_TRIGGER"
	case uint16(CmdPacketSeasonServerUserInfo):
		return "ENUM_CMDPACKET_SEASON_SERVER_USER_INFO"
	case uint16(CmdPacketRequestInfiniteDifficultyInfo):
		return "ENUM_CMDPACKET_REQUEST_INFINITE_DIFFICULTY_INFO"
	case uint16(CmdPacketRequestInfiniteDifficultyCharacInfo):
		return "ENUM_CMDPACKET_REQUEST_INFINITE_DIFFICULTY_CHARAC_INFO"
	case uint16(CmdPacketRequestInfiniteDifficultyRank):
		return "ENUM_CMDPACKET_REQUEST_INFINITE_DIFFICULTY_RANK"
	case uint16(CmdPacketAdventurerMakerCreate):
		return "ENUM_CMDPACKET_ADVENTURER_MAKER_CREATE"
	case uint16(CmdPacketAdventurerMakerGrowRequest):
		return "ENUM_CMDPACKET_ADVENTURER_MAKER_GROW_REQUEST"
	case uint16(CmdPacketAdventurerMakerInitialize):
		return "ENUM_CMDPACKET_ADVENTURER_MAKER_INITIALIZE"
	case uint16(CmdPacketAdventurerMakerGrowRest):
		return "ENUM_CMDPACKET_ADVENTURER_MAKER_GROW_REST"
	case uint16(CmdPacketAdventurerMakerAppearNPC):
		return "ENUM_CMDPACKET_ADVENTURER_MAKER_APPEAR_NPC"
	case uint16(CmdPacketOpenAuraSkinSlot):
		return "ENUM_CMDPACKET_OPEN_AURA_SKIN_SLOT"
	case uint16(CmdPacketCompoundFlag):
		return "ENUM_CMDPACKET_COMPOUND_FLAG"
	case uint16(CmdPacketSeasonCharacInfo):
		return "ENUM_CMDPACKET_SEASON_CHARAC_INFO"
	case uint16(CmdPacketSeasonCharacConvert):
		return "ENUM_CMDPACKET_SEASON_CHARAC_CONVERT"
	case uint16(CmdPacketAccountAchievementReward):
		return "ENUM_CMDPACKET_ACCOUNT_ACHIEVEMENT_REWARD"
	case uint16(CmdPacketSyncItemSpace):
		return "ENUM_CMDPACKET_SYNC_ITEM_SPACE"
	case uint16(CmdPacketSequentialDungeonInfo):
		return "ENUM_CMDPACKET_SEQUENTIAL_DUNGEON_INFO"
	case uint16(CmdPacketCheckPVPPrivateCharacter):
		return "ENUM_CMDPACKET_CHECK_PVP_PRIVATE_CHARACTER"
	case uint16(CmdPacketSetPVPTotalMatchTeam):
		return "ENUM_CMDPACKET_SET_PVP_TOTAL_MATCH_TEAM"
	case uint16(CmdPacketCheckPVPTotalMatchTeamName):
		return "ENUM_CMDPACKET_CHECK_PVP_TOTAL_MATCH_TEAM_NAME"
	case uint16(CmdPacketSetPVPTotalMatchTeamName):
		return "ENUM_CMDPACKET_SET_PVP_TOTAL_MATCH_TEAM_NAME"
	case uint16(CmdPacketSetPVPTotalMatchCharacOrder):
		return "ENUM_CMDPACKET_SET_PVP_TOTAL_MATCH_CHARAC_ORDER"
	case uint16(CmdPacketSetAutoEquipment):
		return "ENUM_CMDPACKET_SET_AUTO_EQUIPMENT"
	case uint16(CmdPacketPVPMissionFirstAerialAttack):
		return "ENUM_CMDPACKET_PVP_MISSION_FIRST_AERIAL_ATTACK"
	case uint16(CmdPacketPVPMissionComboDamageRate):
		return "ENUM_CMDPACKET_PVP_MISSION_COMBO_DAMAGE_RATE"
	case uint16(CmdPacketPVPMissionReplayCount):
		return "ENUM_CMDPACKET_PVP_MISSION_REPLAY_COUNT"
	case uint16(CmdPacketPVPMissionArcadeMode):
		return "ENUM_CMDPACKET_PVP_MISSION_ARCADE_MODE"
	case uint16(CmdPacketTransformationItem):
		return "ENUM_CMDPACKET_TRANSFORMATION_ITEM"
	case uint16(CmdPacketSetPVPUserState):
		return "ENUM_CMDPACKET_SET_PVP_USER_STATE"
	case uint16(CmdPacketReqOpenHiddenQuest):
		return "ENUM_CMDPACKET_REQ_OPEN_HIDDEN_QUEST"
	case uint16(CmdPacketReqSaveEquipInfo4bitanEvent):
		return "ENUM_CMDPACKET_REQ_SAVE_EQUIP_INFO4BITAN_EVENT"
	case uint16(CmdPacketActivateOddSocket):
		return "ENUM_CMDPACKET_ACTIVATE_ODD_SOCKET"
	case uint16(CmdPacketEnchantByOdd):
		return "ENUM_CMDPACKET_ENCHANT_BY_ODD"
	case uint16(CmdPacketCompoundOdd):
		return "ENUM_CMDPACKET_COMPOUND_ODD"
	case uint16(CmdPacketExtractOdd):
		return "ENUM_CMDPACKET_EXTRACT_ODD"
	case uint16(CmdPacketRaidRequestRaidMembers):
		return "ENUM_CMDPACKET_RAID_REQUEST_RAID_MEMBERS"
	case uint16(CmdPacketRaidCheckRaidUser):
		return "ENUM_CMDPACKET_RAID_CHECK_RAID_USER"
	case uint16(CmdPacketSecondPasswordCheck):
		return "ENUM_CMDPACKET_SECOND_PASSWORD_CHECK"
	case uint16(CmdPacketTradingSystem):
		return "ENUM_CMDPACKET_TRADING_SYSTEM"
	case uint16(CmdPacketPassRaidPhase):
		return "ENUM_CMDPACKET_PASS_RAID_PHASE"
	case uint16(CmdPacketHousingTreeInfo):
		return "ENUM_CMDPACKET_HOUSING_TREE_INFO"
	case uint16(CmdPacketHousingSetNewTree):
		return "ENUM_CMDPACKET_HOUSING_SET_NEW_TREE"
	case uint16(CmdPacketHousingGiveWater):
		return "ENUM_CMDPACKET_HOUSING_GIVE_WATER"
	case uint16(CmdPacketHousingHarvestTree):
		return "ENUM_CMDPACKET_HOUSING_HARVEST_TREE"
	case uint16(CmdPacketHousingWaterHistory):
		return "ENUM_CMDPACKET_HOUSING_WATER_HISTORY"
	case uint16(CmdPacketHousingResetTree):
		return "ENUM_CMDPACKET_HOUSING_RESET_TREE"
	case uint16(CmdPacketBuyItemUsePoint):
		return "ENUM_CMDPACKET_BUY_ITEM_USE_POINT"
	case uint16(CmdPacketCheckShopEntrance):
		return "ENUM_CMDPACKET_CHECK_SHOP_ENTRANCE"
	case uint16(CmdPacketEventDnftrendGetReward):
		return "ENUM_CMDPACKET_EVENT_DNFTREND_GET_REWARD"
	case uint16(CmdPacketEventTenMinuteGetReward):
		return "ENUM_CMDPACKET_EVENT_TEN_MINUTE_GET_REWARD"
	case uint16(CmdPacketPremiumService):
		return "ENUM_CMDPACKET_PREMIUM_SERVICE"
	case uint16(CmdPacketAntibotDelayLog):
		return "ENUM_CMDPACKET_ANTIBOT_DELAY_LOG"
	case uint16(CmdPacketEventDungeonDestoryObject):
		return "ENUM_CMDPACKET_EVENT_DUNGEON_DESTORY_OBJECT"
	case uint16(CmdPacketEventDungeonClearRoom):
		return "ENUM_CMDPACKET_EVENT_DUNGEON_CLEAR_ROOM"
	case uint16(CmdPacketChangeCreatureTradeAttr):
		return "ENUM_CMDPACKET_CHANGE_CREATURE_TRADE_ATTR"
	case uint16(CmdPacketLoggingClong):
		return "ENUM_CMDPACKET_LOGGING_CLONG"
	case uint16(CmdPacketMapclearLogWarroom):
		return "ENUM_CMDPACKET_MAPCLEAR_LOG_WARROOM"
	case uint16(CmdPacketStreetVictorBoardTopVictorData):
		return "ENUM_CMDPACKET_STREET_VICTOR_BOARD_TOP_VICTOR_DATA"
	case uint16(CmdPacketStreetVictorBoardMyData):
		return "ENUM_CMDPACKET_STREET_VICTOR_BOARD_MY_DATA"
	case uint16(CmdPacketInformLockTime):
		return "ENUM_CMDPACKET_INFORM_LOCK_TIME"
	case uint16(CmdPacketUseEmblemForEquipment):
		return "ENUM_CMDPACKET_USE_EMBLEM_FOR_EQUIPMENT"
	case uint16(CmdPacketAddEquipmentSocket):
		return "ENUM_CMDPACKET_ADD_EQUIPMENT_SOCKET"
	case uint16(CmdPacketConvertEmblem):
		return "ENUM_CMDPACKET_CONVERT_EMBLEM"
	case uint16(CmdPacketReportBadUser):
		return "ENUM_CMDPACKET_REPORT_BAD_USER"
	case uint16(CmdPacketGetUserInfoForReportBadUser):
		return "ENUM_CMDPACKET_GET_USER_INFO_FOR_REPORT_BAD_USER"
	case uint16(CmdPacketActionPointActionClear):
		return "ENUM_CMDPACKET_ACTION_POINT_ACTION_CLEAR"
	case uint16(CmdPacketActionPointGetRewardItem):
		return "ENUM_CMDPACKET_ACTION_POINT_GET_REWARD_ITEM"
	case uint16(CmdPacketDecorationUseStackable):
		return "ENUM_CMDPACKET_DECORATION_USE_STACKABLE"
	case uint16(CmdPacketDecorationSetup):
		return "ENUM_CMDPACKET_DECORATION_SETUP"
	case uint16(CmdPacketDecorationMoveInvenslot):
		return "ENUM_CMDPACKET_DECORATION_MOVE_INVENSLOT"
	case uint16(CmdPacketHousingInviteFriend):
		return "ENUM_CMDPACKET_HOUSING_INVITE_FRIEND"
	case uint16(CmdPacketHousingVisitRoom):
		return "ENUM_CMDPACKET_HOUSING_VISIT_ROOM"
	case uint16(CmdPacketHousingEnterRoom):
		return "ENUM_CMDPACKET_HOUSING_ENTER_ROOM"
	case uint16(CmdPacketHousingLeaveRoom):
		return "ENUM_CMDPACKET_HOUSING_LEAVE_ROOM"
	case uint16(CmdPacketHousingSendMessage):
		return "ENUM_CMDPACKET_HOUSING_SEND_MESSAGE"
	case uint16(CmdPacketPacketSwitchEquipslot):
		return "ENUM_CMDPACKET_PACKET_SWITCH_EQUIPSLOT"
	case uint16(CmdPacketKillPlayerLog):
		return "ENUM_CMDPACKET_KILL_PLAYER_LOG"
	case uint16(CmdPacketJoinTournament):
		return "ENUM_CMDPACKET_JOIN_TOURNAMENT"
	case uint16(CmdPacketTournamentStatus):
		return "ENUM_CMDPACKET_TOURNAMENT_STATUS"
	case uint16(CmdPacketUseAvatarPottery):
		return "ENUM_CMDPACKET_USE_AVATAR_POTTERY"
	case uint16(CmdPacketMoveItemToAccountCargo):
		return "ENUM_CMDPACKET_MOVE_ITEM_TO_ACCOUNT_CARGO"
	case uint16(CmdPacketAradAttendanceCheck):
		return "ENUM_CMDPACKET_ARAD_ATTENDANCE_CHECK"
	case uint16(CmdPacketAradCompoundKatagaki):
		return "ENUM_CMDPACKET_ARAD_COMPOUND_KATAGAKI"
	case uint16(CmdPacketEventLoginAndGift):
		return "ENUM_CMDPACKET_EVENT_LOGIN_AND_GIFT"
	case uint16(CmdPacketUsedGoldskill):
		return "ENUM_CMDPACKET_USED_GOLDSKILL"
	case uint16(CmdPacketAdvanceAltarStartGame):
		return "ENUM_CMDPACKET_ADVANCE_ALTAR_START_GAME"
	case uint16(CmdPacketAdvanceAltarBuyItem):
		return "ENUM_CMDPACKET_ADVANCE_ALTAR_BUY_ITEM"
	case uint16(CmdPacketAdvanceAltarSetSlot):
		return "ENUM_CMDPACKET_ADVANCE_ALTAR_SET_SLOT"
	case uint16(CmdPacketAdvanceAltarUpgradeGage):
		return "ENUM_CMDPACKET_ADVANCE_ALTAR_UPGRADE_GAGE"
	case uint16(CmdPacketAdvanceAltarSummonUnit):
		return "ENUM_CMDPACKET_ADVANCE_ALTAR_SUMMON_UNIT"
	case uint16(CmdPacketAdvanceAltarExchangeSlot):
		return "ENUM_CMDPACKET_ADVANCE_ALTAR_EXCHANGE_SLOT"
	case uint16(CmdPacketAdvanceAltarPause):
		return "ENUM_CMDPACKET_ADVANCE_ALTAR_PAUSE"
	case uint16(CmdPacketAdvanceAltarGetAchievementReward):
		return "ENUM_CMDPACKET_ADVANCE_ALTAR_GET_ACHIEVEMENT_REWARD"
	case uint16(CmdPacketAdvanceAltarResetStar):
		return "ENUM_CMDPACKET_ADVANCE_ALTAR_RESET_STAR"
	case uint16(CmdPacketAdvanceAltarStageClearInfo):
		return "ENUM_CMDPACKET_ADVANCE_ALTAR_STAGE_CLEAR_INFO"
	case uint16(CmdPacketAdvanceAltarFullGageReward):
		return "ENUM_CMDPACKET_ADVANCE_ALTAR_FULL_GAGE_REWARD"
	case uint16(CmdPacketReqIgaVer):
		return "ENUM_CMDPACKET_REQ_IGA_VER"
	case uint16(CmdPacketPeriodItemRebuyStatistic):
		return "ENUM_CMDPACKET_PERIOD_ITEM_REBUY_STATISTIC"
	case uint16(CmdPacketAddEquipmentEffect):
		return "ENUM_CMDPACKET_ADD_EQUIPMENT_EFFECT"
	case uint16(CmdPacketMinorityEvent):
		return "ENUM_CMDPACKET_MINORITY_EVENT"
	case uint16(CmdPacketUseJumpingCharTicket):
		return "ENUM_CMDPACKET_USE_JUMPING_CHAR_TICKET"
	case uint16(CmdPacketUseAvatarRoulette):
		return "ENUM_CMDPACKET_USE_AVATAR_ROULETTE"
	case uint16(CmdPacketAvatarCoinCount):
		return "ENUM_CMDPACKET_AVATAR_COIN_COUNT"
	case uint16(CmdPacketAvatarHiddenOptionChange):
		return "ENUM_CMDPACKET_AVATAR_HIDDEN_OPTION_CHANGE"
	case uint16(CmdPacketUseAvatarRechargeJpn):
		return "ENUM_CMDPACKET_USE_AVATAR_RECHARGE_JPN"
	case uint16(CmdPacketEmblemCompoundJpn):
		return "ENUM_CMDPACKET_EMBLEM_COMPOUND_JPN"
	case uint16(CmdPacketAvatarConvertJpn):
		return "ENUM_CMDPACKET_AVATAR_CONVERT_JPN"
	case uint16(CmdPacketReqAradConditionEventReward):
		return "ENUM_CMDPACKET_REQ_ARAD_CONDITION_EVENT_REWARD"
	case uint16(CmdPacketUseAncientMysticCube):
		return "ENUM_CMDPACKET_USE_ANCIENT_MYSTIC_CUBE"
	case uint16(CmdPacketQuizReqItem):
		return "ENUM_CMDPACKET_QUIZ_REQ_ITEM"
	case uint16(CmdPacketQuizResItem):
		return "ENUM_CMDPACKET_QUIZ_RES_ITEM"
	case uint16(CmdPacketQuestBefore70LevelExpand):
		return "ENUM_CMDPACKET_QUEST_BEFORE_70_LEVEL_EXPAND"
	case uint16(CmdPacketLevelupSupportReqItem):
		return "ENUM_CMDPACKET_LEVELUP_SUPPORT_REQ_ITEM"
	case uint16(CmdPacketP2pHolePunchingSuccessRate):
		return "ENUM_CMDPACKET_P2P_HOLE_PUNCHING_SUCCESS_RATE"
	case uint16(CmdPacketCharacterCreateCountPerDay):
		return "ENUM_CMDPACKET_CHARACTER_CREATE_COUNT_PER_DAY"
	case uint16(CmdPacketUseTitleChangeItem):
		return "ENUM_CMDPACKET_USE_TITLE_CHANGE_ITEM"
	case uint16(CmdPacketGetDanjinEventAvatarLimitBox):
		return "ENUM_CMDPACKET_GET_DANJIN_EVENT_AVATAR_LIMIT_BOX"
	case uint16(CmdPacketGetDanjinEventAvatarUnlimitBox):
		return "ENUM_CMDPACKET_GET_DANJIN_EVENT_AVATAR_UNLIMIT_BOX"
	case uint16(CmdPacketUseDanjinEventAvatarExtend):
		return "ENUM_CMDPACKET_USE_DANJIN_EVENT_AVATAR_EXTEND"
	case uint16(CmdPacketDestoryDungeonStart):
		return "ENUM_CMDPACKET_DESTORY_DUNGEON_START"
	case uint16(CmdPacketCompoundUniqueItem):
		return "ENUM_CMDPACKET_COMPOUND_UNIQUE_ITEM"
	case uint16(CmdPacketDoubleAttendanceSetLogout):
		return "ENUM_CMDPACKET_DOUBLE_ATTENDANCE_SET_LOGOUT"
	case uint16(CmdPacketReportPartyPlayManner):
		return "ENUM_CMDPACKET_REPORT_PARTY_PLAY_MANNER"
	case uint16(CmdPacketEmblemCollectItem):
		return "ENUM_CMDPACKET_EMBLEM_COLLECT_ITEM"
	case uint16(CmdPacketReadHuntingSkill):
		return "ENUM_CMDPACKET_READ_HUNTING_SKILL"
	case uint16(CmdPacketUpgradeCarryGold):
		return "ENUM_CMDPACKET_UPGRADE_CARRY_GOLD"
	case uint16(CmdPacketApcTnInfo):
		return "ENUM_CMDPACKET_APC_TN_INFO"
	case uint16(CmdPacketApcTnEntrance):
		return "ENUM_CMDPACKET_APC_TN_ENTRANCE"
	case uint16(CmdPacketApcTnBetting):
		return "ENUM_CMDPACKET_APC_TN_BETTING"
	case uint16(CmdPacketApcTnDividend):
		return "ENUM_CMDPACKET_APC_TN_DIVIDEND"
	case uint16(CmdPacketRequestPickedAvatar):
		return "ENUM_CMDPACKET_REQUEST_PICKED_AVATAR"
	case uint16(CmdPacketSecretEventShopBuyItem):
		return "ENUM_CMDPACKET_SECRET_EVENT_SHOP_BUY_ITEM"
	case uint16(CmdPacketUserChoiceEvent):
		return "ENUM_CMDPACKET_USER_CHOICE_EVENT"
	case uint16(CmdPacketEventAttendanceReward):
		return "ENUM_CMDPACKET_EVENT_ATTENDANCE_REWARD"
	case uint16(CmdPacketQqPcroomBenefitReward):
		return "ENUM_CMDPACKET_QQ_PCROOM_BENEFIT_REWARD"
	case uint16(CmdPacketEventCreateDnfRequest):
		return "ENUM_CMDPACKET_EVENT_CREATE_DNF_REQUEST"
	case uint16(CmdPacketRequestPcroomDayilyReward):
		return "ENUM_CMDPACKET_REQUEST_PCROOM_DAYILY_REWARD"
	case uint16(CmdPacketHeroMissionDataReward):
		return "ENUM_CMDPACKET_HERO_MISSION_DATA_REWARD"
	case uint16(CmdPacketRogerLevineAuctionBuynow):
		return "ENUM_CMDPACKET_ROGER_LEVINE_AUCTION_BUYNOW"
	case uint16(CmdPacketRogerLevineAuctionBidding):
		return "ENUM_CMDPACKET_ROGER_LEVINE_AUCTION_BIDDING"
	case uint16(CmdPacketDungeonNPCShopBuyItem):
		return "ENUM_CMDPACKET_DUNGEON_NPC_SHOP_BUY_ITEM"
	case uint16(CmdPacketDungeonNPCShopOpenClose):
		return "ENUM_CMDPACKET_DUNGEON_NPC_SHOP_OPEN_CLOSE"
	case uint16(CmdPacketSeventhMissionReward):
		return "ENUM_CMDPACKET_SEVENTH_MISSION_REWARD"
	case uint16(CmdPacketSeriaRidableInHiddenTruthDungeon):
		return "ENUM_CMDPACKET_SERIA_RIDABLE_IN_HIDDEN_TRUTH_DUNGEON"
	case uint16(CmdPacketDungeonClearReward):
		return "ENUM_CMDPACKET_DUNGEON_CLEAR_REWARD"
	case uint16(CmdPacketBuyMoonFestivalItem):
		return "ENUM_CMDPACKET_BUY_MOON_FESTIVAL_ITEM"
	case uint16(CmdPacketRentEquipmentItem):
		return "ENUM_CMDPACKET_RENT_EQUIPMENT_ITEM"
	case uint16(CmdPacketChargeRentpoint):
		return "ENUM_CMDPACKET_CHARGE_RENTPOINT"
	case uint16(CmdPacketRequestReceiveAttandanceReward):
		return "ENUM_CMDPACKET_REQUEST_RECEIVE_ATTANDANCE_REWARD"
	case uint16(CmdPacketEventAccountFatigueStat):
		return "ENUM_CMDPACKET_EVENT_ACCOUNT_FATIGUE_STAT"
	case uint16(CmdPacketGoldenEggRewardRequest):
		return "ENUM_CMDPACKET_GOLDEN_EGG_REWARD_REQUEST"
	case uint16(CmdPacketLevelupSupport3rdEventGetItem):
		return "ENUM_CMDPACKET_LEVELUP_SUPPORT_3RD_EVENT_GET_ITEM"
	case uint16(CmdPacketJuly7thRequestResponse):
		return "ENUM_CMDPACKET_JULY7TH_REQUEST_RESPONSE"
	case uint16(CmdPacketDecideChallengeAttendance):
		return "ENUM_CMDPACKET_DECIDE_CHALLENGE_ATTENDANCE"
	case uint16(CmdPacketTictactoeMarking):
		return "ENUM_CMDPACKET_TICTACTOE_MARKING"
	case uint16(CmdPacketTictactoeRequestData):
		return "ENUM_CMDPACKET_TICTACTOE_REQUEST_DATA"
	case uint16(CmdPacketTictactoeReward):
		return "ENUM_CMDPACKET_TICTACTOE_REWARD"
	case uint16(CmdPacketExchangeItemFromNPC):
		return "ENUM_CMDPACKET_EXCHANGE_ITEM_FROM_NPC"
	case uint16(CmdPacketChainLettersReward):
		return "ENUM_CMDPACKET_CHAIN_LETTERS_REWARD"
	case uint16(CmdPacketSealCreature):
		return "ENUM_CMDPACKET_SEAL_CREATURE"
	case uint16(CmdPacketDnfWithFriendsRecommend):
		return "ENUM_CMDPACKET_DNF_WITH_FRIENDS_RECOMMEND"
	case uint16(CmdPacketDnfWithFriendsInfo):
		return "ENUM_CMDPACKET_DNF_WITH_FRIENDS_INFO"
	case uint16(CmdPacketDnfWithFriendsBuy):
		return "ENUM_CMDPACKET_DNF_WITH_FRIENDS_BUY"
	case uint16(CmdPacketHelpLevelUp):
		return "ENUM_CMDPACKET_HELP_LEVEL_UP"
	case uint16(CmdPacketEventGrowthEquipment):
		return "ENUM_CMDPACKET_EVENT_GROWTH_EQUIPMENT"
	case uint16(CmdPacketShootingstarBuyItem):
		return "ENUM_CMDPACKET_SHOOTINGSTAR_BUY_ITEM"
	case uint16(CmdPacketDimensionPocketItem):
		return "ENUM_CMDPACKET_DIMENSION_POCKET_ITEM"
	case uint16(CmdPacketAccelerateGrowthSendGift):
		return "ENUM_CMDPACKET_ACCELERATE_GROWTH_SEND_GIFT"
	case uint16(CmdPacketSelectCollectbox):
		return "ENUM_CMDPACKET_SELECT_COLLECTBOX"
	case uint16(CmdPacketAddCollectboxItem):
		return "ENUM_CMDPACKET_ADD_COLLECTBOX_ITEM"
	case uint16(CmdPacketRemoveCollectboxItem):
		return "ENUM_CMDPACKET_REMOVE_COLLECTBOX_ITEM"
	case uint16(CmdPacketExtendCollectboxExpiryDate):
		return "ENUM_CMDPACKET_EXTEND_COLLECTBOX_EXPIRY_DATE"
	case uint16(CmdPacketTakeMagicLamp):
		return "ENUM_CMDPACKET_TAKE_MAGIC_LAMP"
	case uint16(CmdPacketSendGlassBottleLetter):
		return "ENUM_CMDPACKET_SEND_GLASS_BOTTLE_LETTER"
	case uint16(CmdPacketFacebookLike):
		return "ENUM_CMDPACKET_FACEBOOK_LIKE"
	case uint16(CmdPacketOnTimeImdRequest):
		return "ENUM_CMDPACKET_ON_TIME_IMD_REQUEST"
	case uint16(CmdPacketTheSeaBottom2000Use):
		return "ENUM_CMDPACKET_THE_SEA_BOTTOM2000_USE"
	case uint16(CmdPacketUniqueBindShpereInfo):
		return "ENUM_CMDPACKET_UNIQUE_BIND_SHPERE_INFO"
	case uint16(CmdPacketPrepareNewBalance):
		return "ENUM_CMDPACKET_PREPARE_NEW_BALANCE"
	case uint16(CmdPacketWeddingRequest):
		return "ENUM_CMDPACKET_WEDDING_REQUEST"
	case uint16(CmdPacketWeddingResponse):
		return "ENUM_CMDPACKET_WEDDING_RESPONSE"
	case uint16(CmdPacketWeddingMoneyGift):
		return "ENUM_CMDPACKET_WEDDING_MONEY_GIFT"
	case uint16(CmdPacketWeddingCharac):
		return "ENUM_CMDPACKET_WEDDING_CHARAC"
	case uint16(CmdPacketWeddingUpgradeRing):
		return "ENUM_CMDPACKET_WEDDING_UPGRADE_RING"
	case uint16(CmdPacketWeddingUseLovePointItem):
		return "ENUM_CMDPACKET_WEDDING_USE_LOVE_POINT_ITEM"
	case uint16(CmdPacketWeddingSendLetter):
		return "ENUM_CMDPACKET_WEDDING_SEND_LETTER"
	case uint16(CmdPacketWeddingEnterCeremony):
		return "ENUM_CMDPACKET_WEDDING_ENTER_CEREMONY"
	case uint16(CmdPacketUpdateGuestComment):
		return "ENUM_CMDPACKET_UPDATE_GUEST_COMMENT"
	case uint16(CmdPacketDeleteGuestComment):
		return "ENUM_CMDPACKET_DELETE_GUEST_COMMENT"
	case uint16(CmdPacketLoadGuestComment):
		return "ENUM_CMDPACKET_LOAD_GUEST_COMMENT"
	case uint16(CmdPacketCheckMoveToPartner):
		return "ENUM_CMDPACKET_CHECK_MOVE_TO_PARTNER"
	case uint16(CmdPacketMoveToPartner):
		return "ENUM_CMDPACKET_MOVE_TO_PARTNER"
	case uint16(CmdPacketCoupleInvenOpenClose):
		return "ENUM_CMDPACKET_COUPLE_INVEN_OPEN_CLOSE"
	case uint16(CmdPacketCoupleRoomMoveItem):
		return "ENUM_CMDPACKET_COUPLE_ROOM_MOVE_ITEM"
	case uint16(CmdPacketCoupleInvenDeleteItem):
		return "ENUM_CMDPACKET_COUPLE_INVEN_DELETE_ITEM"
	case uint16(CmdPacketCoupleTreePlant):
		return "ENUM_CMDPACKET_COUPLE_TREE_PLANT"
	case uint16(CmdPacketCoupleTreeWater):
		return "ENUM_CMDPACKET_COUPLE_TREE_WATER"
	case uint16(CmdPacketCoupleTreeHarvest):
		return "ENUM_CMDPACKET_COUPLE_TREE_HARVEST"
	case uint16(CmdPacketCoupleTreeRemove):
		return "ENUM_CMDPACKET_COUPLE_TREE_REMOVE"
	case uint16(CmdPacketMakeCoupleGrowthcapsule):
		return "ENUM_CMDPACKET_MAKE_COUPLE_GROWTHCAPSULE"
	case uint16(CmdPacketGiveCoupleGrowthcapsule):
		return "ENUM_CMDPACKET_GIVE_COUPLE_GROWTHCAPSULE"
	case uint16(CmdPacketTakeCoupleGrowthcapsule):
		return "ENUM_CMDPACKET_TAKE_COUPLE_GROWTHCAPSULE"
	case uint16(CmdPacketDragonDungeonTimeattackRankerBoard):
		return "ENUM_CMDPACKET_DRAGON_DUNGEON_TIMEATTACK_RANKER_BOARD"
	case uint16(CmdPacketBreakTrapResult):
		return "ENUM_CMDPACKET_BREAK_TRAP_RESULT"
	case uint16(CmdPacketEventChangeClass):
		return "ENUM_CMDPACKET_EVENT_CHANGE_CLASS"
	case uint16(CmdPacketTripleExpKeepingReward):
		return "ENUM_CMDPACKET_TRIPLE_EXP_KEEPING_REWARD"
	case uint16(CmdPacketSelectRandomFortune):
		return "ENUM_CMDPACKET_SELECT_RANDOM_FORTUNE"
	case uint16(CmdPacketPlayBlueHorseSlot):
		return "ENUM_CMDPACKET_PLAY_BLUE_HORSE_SLOT"
	case uint16(CmdPacketRewardBlueHorseSlot):
		return "ENUM_CMDPACKET_REWARD_BLUE_HORSE_SLOT"
	case uint16(CmdPacketMysticAvatarAlpha):
		return "ENUM_CMDPACKET_MYSTIC_AVATAR_ALPHA"
	case uint16(CmdPacketYearEndLotto):
		return "ENUM_CMDPACKET_YEAR_END_LOTTO"
	case uint16(CmdPacketBingoStampInfo):
		return "ENUM_CMDPACKET_BINGO_STAMP_INFO"
	case uint16(CmdPacketRetryBooterInfo):
		return "ENUM_CMDPACKET_RETRY_BOOTER_INFO"
	case uint16(CmdPacketOlympicMedalInfo):
		return "ENUM_CMDPACKET_OLYMPIC_MEDAL_INFO"
	case uint16(CmdPacketVeryDifficultHellParty):
		return "ENUM_CMDPACKET_VERY_DIFFICULT_HELL_PARTY"
	case uint16(CmdPacketLevelDecision):
		return "ENUM_CMDPACKET_LEVEL_DECISION"
	case uint16(CmdPacketCompoundForDiametricallyItem):
		return "ENUM_CMDPACKET_COMPOUND_FOR_DIAMETRICALLY_ITEM"
	case uint16(CmdPacketUniqueAdvancedBindShpere):
		return "ENUM_CMDPACKET_UNIQUE_ADVANCED_BIND_SHPERE"
	case uint16(CmdPacketEventQuestion):
		return "ENUM_CMDPACKET_EVENT_QUESTION"
	case uint16(CmdPacketDoblinFromStar):
		return "ENUM_CMDPACKET_DOBLIN_FROM_STAR"
	case uint16(CmdPacketMarbleDiceInfo):
		return "ENUM_CMDPACKET_MARBLE_DICE_INFO"
	case uint16(CmdPacketMarbleDiceThrow):
		return "ENUM_CMDPACKET_MARBLE_DICE_THROW"
	case uint16(CmdPacketMarbleDiceExit):
		return "ENUM_CMDPACKET_MARBLE_DICE_EXIT"
	case uint16(CmdPacketTeraPieceRank):
		return "ENUM_CMDPACKET_TERA_PIECE_RANK"
	case uint16(CmdPacketExchangeCeraPoint):
		return "ENUM_CMDPACKET_EXCHANGE_CERA_POINT"
	case uint16(CmdPacketJobLevelDecisionSelect):
		return "ENUM_CMDPACKET_JOB_LEVEL_DECISION_SELECT"
	case uint16(CmdPacketJobLevelDecisionInfo):
		return "ENUM_CMDPACKET_JOB_LEVEL_DECISION_INFO"
	case uint16(CmdPacketJobLevelDecisionChange):
		return "ENUM_CMDPACKET_JOB_LEVEL_DECISION_CHANGE"
	case uint16(CmdPacketPhoenixWeaponEventReward):
		return "ENUM_CMDPACKET_PHOENIX_WEAPON_EVENT_REWARD"
	case uint16(CmdPacketPhoenixWeaponEventRewardMailCheckCharac):
		return "ENUM_CMDPACKET_PHOENIX_WEAPON_EVENT_REWARD_MAIL_CHECK_CHARAC"
	case uint16(CmdPacketPhoenixWeaponEventRewardMail):
		return "ENUM_CMDPACKET_PHOENIX_WEAPON_EVENT_REWARD_MAIL"
	case uint16(CmdPacketPhoenixWeaponEventChangePeriod):
		return "ENUM_CMDPACKET_PHOENIX_WEAPON_EVENT_CHANGE_PERIOD"
	case uint16(CmdPacketPhoenixWeaponEventChangeWeapon):
		return "ENUM_CMDPACKET_PHOENIX_WEAPON_EVENT_CHANGE_WEAPON"
	case uint16(CmdPacketFindSignsFinalRewardEvent):
		return "ENUM_CMDPACKET_FIND_SIGNS_FINAL_REWARD_EVENT"
	case uint16(CmdPacketCelebrateBigTransitionEventReward):
		return "ENUM_CMDPACKET_CELEBRATE_BIG_TRANSITION_EVENT_REWARD"
	case uint16(CmdPacket6thGoldcoin):
		return "ENUM_CMDPACKET_6TH_GOLDCOIN_"
	case uint16(CmdPacketUpdateBestDamageRank):
		return "ENUM_CMDPACKET_UPDATE_BEST_DAMAGE_RANK"
	case uint16(CmdPacketRequestBestDamageRank):
		return "ENUM_CMDPACKET_REQUEST_BEST_DAMAGE_RANK"
	case uint16(CmdPacketSenseOfChoice):
		return "ENUM_CMDPACKET_SENSE_OF_CHOICE"
	case uint16(CmdPacketDiceGame):
		return "ENUM_CMDPACKET_DICE_GAME"
	case uint16(CmdPacketValentineEvent2014):
		return "ENUM_CMDPACKET_VALENTINE_EVENT_2014"
	case uint16(CmdPacket2014LieDayDelilahShopBuyJpn):
		return "ENUM_CMDPACKET_2014_LIE_DAY_DELILAH_SHOP_BUY_JPN"
	case uint16(CmdPacketEventSugartimeDayReward):
		return "ENUM_CMDPACKET_EVENT_SUGARTIME_DAY_REWARD"
	case uint16(CmdPacketEventSugartimeFinalReward):
		return "ENUM_CMDPACKET_EVENT_SUGARTIME_FINAL_REWARD"
	case uint16(CmdPacketEventSugartimeFinalPick):
		return "ENUM_CMDPACKET_EVENT_SUGARTIME_FINAL_PICK"
	case uint16(CmdPacketDualRaidBossHpCheck):
		return "ENUM_CMDPACKET_DUAL_RAID_BOSS_HP_CHECK"
	case uint16(CmdPacketCreatureExpBook):
		return "ENUM_CMDPACKET_CREATURE_EXP_BOOK"
	case uint16(CmdPacketUpgradeTicketSlotMachinePlay):
		return "ENUM_CMDPACKET_UPGRADE_TICKET_SLOT_MACHINE_PLAY"
	case uint16(CmdPacketUpgradeTicketSlotMachineOpen):
		return "ENUM_CMDPACKET_UPGRADE_TICKET_SLOT_MACHINE_OPEN"
	case uint16(CmdPacketPuzzleGameChoice):
		return "ENUM_CMDPACKET_PUZZLE_GAME_CHOICE"
	case uint16(CmdPacketPuzzleChanceReward):
		return "ENUM_CMDPACKET_PUZZLE_CHANCE_REWARD"
	case uint16(CmdPacketTosCurDungeonInfo):
		return "ENUM_CMDPACKET_TOS_CUR_DUNGEON_INFO"
	case uint16(CmdPacketFatigueStepReward):
		return "ENUM_CMDPACKET_FATIGUE_STEP_REWARD"
	case uint16(CmdPacketMissionRoulette):
		return "ENUM_CMDPACKET_MISSION_ROULETTE"
	case uint16(CmdPacketMissionRouletteTrigger):
		return "ENUM_CMDPACKET_MISSION_ROULETTE_TRIGGER"
	case uint16(CmdPacketChangeAvatarClosetSet):
		return "ENUM_CMDPACKET_CHANGE_AVATAR_CLOSET_SET"
	case uint16(CmdPacketSelectAvatarCloset):
		return "ENUM_CMDPACKET_SELECT_AVATAR_CLOSET"
	case uint16(CmdPacketGunner2ndAttendanceRewardReq):
		return "ENUM_CMDPACKET_GUNNER2ND_ATTENDANCE_REWARD_REQ"
	case uint16(CmdPacketChargingCharmEnergy):
		return "ENUM_CMDPACKET_CHARGING_CHARM_ENERGY"
	case uint16(CmdPacketEventQuickSlotReward):
		return "ENUM_CMDPACKET_EVENT_QUICK_SLOT_REWARD"
	case uint16(CmdPacketEventTotalAttendanceCheckThisweek):
		return "ENUM_CMDPACKET_EVENT_TOTAL_ATTENDANCE_CHECK_THISWEEK"
	case uint16(CmdPacketLegendSwordInfo):
		return "ENUM_CMDPACKET_LEGEND_SWORD_INFO"
	case uint16(CmdPacketRequestRewardCompleteDecideLevelup):
		return "ENUM_CMDPACKET_REQUEST_REWARD_COMPLETE_DECIDE_LEVELUP"
	case uint16(CmdPacketRequestRewardCompleteDecideState):
		return "ENUM_CMDPACKET_REQUEST_REWARD_COMPLETE_DECIDE_STATE"
	case uint16(CmdPacketUpgradeAvatarCloset):
		return "ENUM_CMDPACKET_UPGRADE_AVATAR_CLOSET"
	case uint16(CmdPacketHalloweenInfo):
		return "ENUM_CMDPACKET_HALLOWEEN_INFO"
	case uint16(CmdPacketBindPlus):
		return "ENUM_CMDPACKET_BIND_PLUS"
	case uint16(CmdPacketCardCompound):
		return "ENUM_CMDPACKET_CARD_COMPOUND"
	case uint16(CmdPacketDonateForEco):
		return "ENUM_CMDPACKET_DONATE_FOR_ECO"
	case uint16(CmdPacketEventItemRequest):
		return "ENUM_CMDPACKET_EVENT_ITEM_REQUEST"
	case uint16(CmdPacketAttendanceCalendar):
		return "ENUM_CMDPACKET_ATTENDANCE_CALENDAR"
	case uint16(CmdPacketXigncodeSecurityData):
		return "ENUM_CMDPACKET_XIGNCODE_SECURITY_DATA"
	case uint16(CmdPacketChristmasPresentExchange):
		return "ENUM_CMDPACKET_CHRISTMAS_PRESENT_EXCHANGE"
	case uint16(CmdPacketLongAttendanceChangeMission):
		return "ENUM_CMDPACKET_LONG_ATTENDANCE_CHANGE_MISSION"
	case uint16(CmdPacketProperDungeonClearCharacReward):
		return "ENUM_CMDPACKET_PROPER_DUNGEON_CLEAR_CHARAC_REWARD"
	case uint16(CmdPacketUseRandomboxItemExpand):
		return "ENUM_CMDPACKET_USE_RANDOMBOX_ITEM_EXPAND"
	case uint16(CmdPacketBurningUpToSpecificLevel):
		return "ENUM_CMDPACKET_BURNING_UP_TO_SPECIFIC_LEVEL"
	case uint16(CmdPacketPeerPingIndexUniv):
		return "ENUM_CMDPACKET_PEER_PING_INDEX_UNIV"
	case uint16(CmdPacketIncreaseChanceLotteryReset):
		return "ENUM_CMDPACKET_INCREASE_CHANCE_LOTTERY_RESET"
	case uint16(CmdPacketGoalAttainment):
		return "ENUM_CMDPACKET_GOAL_ATTAINMENT"
	case uint16(CmdPacketChronicleDisjoint):
		return "ENUM_CMDPACKET_CHRONICLE_DISJOINT"
	case uint16(CmdPacketMahjongStartgame):
		return "ENUM_CMDPACKET_MAHJONG_STARTGAME"
	case uint16(CmdPacketMahjongGet):
		return "ENUM_CMDPACKET_MAHJONG_GET"
	case uint16(CmdPacketMahjongDrop):
		return "ENUM_CMDPACKET_MAHJONG_DROP"
	case uint16(CmdPacketAimForTheLegendary):
		return "ENUM_CMDPACKET_AIM_FOR_THE_LEGENDARY"
	case uint16(CmdPacketPlayDnfAtLaborday):
		return "ENUM_CMDPACKET_PLAY_DNF_AT_LABORDAY"
	case uint16(CmdPacketProperDungeonCountAccount):
		return "ENUM_CMDPACKET_PROPER_DUNGEON_COUNT_ACCOUNT"
	case uint16(CmdPacketFramelagPerDungeon):
		return "ENUM_CMDPACKET_FRAMELAG_PER_DUNGEON"
	case uint16(CmdPacketPresentToPeris):
		return "ENUM_CMDPACKET_PRESENT_TO_PERIS"
	case uint16(CmdPacketMachineCreatureChangeState):
		return "ENUM_CMDPACKET_MACHINE_CREATURE_CHANGE_STATE"
	case uint16(CmdPacketAccountOnceGiftEvent):
		return "ENUM_CMDPACKET_ACCOUNT_ONCE_GIFT_EVENT"
	case uint16(CmdPacketDungeonBossMapSelect):
		return "ENUM_CMDPACKET_DUNGEON_BOSS_MAP_SELECT"
	case uint16(CmdPacketWelcombackAttendance):
		return "ENUM_CMDPACKET_WELCOMBACK_ATTENDANCE"
	case uint16(CmdPacketWelcombackDailyMission):
		return "ENUM_CMDPACKET_WELCOMBACK_DAILY_MISSION"
	case uint16(CmdPacketFoolsDaySyusiaPresent):
		return "ENUM_CMDPACKET_FOOLS_DAY_SYUSIA_PRESENT"
	case uint16(CmdPacketWelcombackDailyEquipReward):
		return "ENUM_CMDPACKET_WELCOMBACK_DAILY_EQUIP_REWARD"
	case uint16(CmdPacketAddBuffVendingMachine):
		return "ENUM_CMDPACKET_ADD_BUFF_VENDING_MACHINE"
	case uint16(CmdPacketKunoichiHotTraining):
		return "ENUM_CMDPACKET_KUNOICHI_HOT_TRAINING"
	case uint16(CmdPacketNinjaSmithy):
		return "ENUM_CMDPACKET_NINJA_SMITHY"
	case uint16(CmdPacketGoldenBingoEvent):
		return "ENUM_CMDPACKET_GOLDEN_BINGO_EVENT"
	case uint16(CmdPacketEveryDayDfo):
		return "ENUM_CMDPACKET_EVERY_DAY_DFO"
	case uint16(CmdPacketSecurityCardEmailReqUniv):
		return "ENUM_CMDPACKET_SECURITY_CARD_EMAIL_REQ_UNIV"
	case uint16(CmdPacketPartyTimeUniv):
		return "ENUM_CMDPACKET_PARTY_TIME_UNIV"
	case uint16(CmdPacketSecurityCardCertKeyCancelUniv):
		return "ENUM_CMDPACKET_SECURITY_CARD_CERT_KEY_CANCEL_UNIV"
	case uint16(CmdPacketPriestMissionReward):
		return "ENUM_CMDPACKET_PRIEST_MISSION_REWARD"
	case uint16(CmdPacketPriestLevelupSupport):
		return "ENUM_CMDPACKET_PRIEST_LEVELUP_SUPPORT"
	case uint16(CmdPacketPriestDimensionSupport):
		return "ENUM_CMDPACKET_PRIEST_DIMENSION_SUPPORT"
	case uint16(CmdPacketLiaWalkieTalkieAttendence):
		return "ENUM_CMDPACKET_LIA_WALKIE_TALKIE_ATTENDENCE"
	case uint16(CmdPacketMysticAvatar):
		return "ENUM_CMDPACKET_MYSTIC_AVATAR"
	case uint16(CmdPacketCommonStructSample):
		return "ENUM_CMDPACKET_COMMON_STRUCT_SAMPLE"
	case uint16(CmdPacketSelectTimeAttendance):
		return "ENUM_CMDPACKET_SELECT_TIME_ATTENDANCE"
	case uint16(CmdPacketGodGrowthSupport):
		return "ENUM_CMDPACKET_GOD_GROWTH_SUPPORT"
	case uint16(CmdPacketKaronLetheFree):
		return "ENUM_CMDPACKET_KARON_LETHE_FREE"
	case uint16(CmdPacketCommonBuffVendingMachineChn):
		return "ENUM_CMDPACKET_COMMON_BUFF_VENDING_MACHINE_CHN"
	case uint16(CmdPacketAllUserGrowup):
		return "ENUM_CMDPACKET_ALL_USER_GROWUP"
	case uint16(CmdPacket7thAnniversary):
		return "ENUM_CMDPACKET_7TH_ANNIVERSARY"
	case uint16(CmdPacketNewAccountRecommandFriend):
		return "ENUM_CMDPACKET_NEW_ACCOUNT_RECOMMAND_FRIEND"
	case uint16(CmdPacketNewAccountReqRecommandCountReward):
		return "ENUM_CMDPACKET_NEW_ACCOUNT_REQ_RECOMMAND_COUNT_REWARD"
	case uint16(CmdPacketSeriaClosetBuy):
		return "ENUM_CMDPACKET_SERIA_CLOSET_BUY"
	case uint16(CmdPacketSeriaClosetWear):
		return "ENUM_CMDPACKET_SERIA_CLOSET_WEAR"
	case uint16(CmdPacketReqCircusDungeonTicket):
		return "ENUM_CMDPACKET_REQ_CIRCUS_DUNGEON_TICKET"
	case uint16(CmdPacketMakeUnderworldMapPiece):
		return "ENUM_CMDPACKET_MAKE_UNDERWORLD_MAP_PIECE"
	case uint16(CmdPacketReqUnderworldMapReward):
		return "ENUM_CMDPACKET_REQ_UNDERWORLD_MAP_REWARD"
	case uint16(CmdPacketUsePayletterCoupon):
		return "ENUM_CMDPACKET_USE_PAYLETTER_COUPON"
	case uint16(CmdPacket8weekAttendance):
		return "ENUM_CMDPACKET_8WEEK_ATTENDANCE"
	case uint16(CmdPacketAtswordmanPhoenixWeapon):
		return "ENUM_CMDPACKET_ATSWORDMAN_PHOENIX_WEAPON"
	case uint16(CmdPacketChangePhoenixWeapon):
		return "ENUM_CMDPACKET_CHANGE_PHOENIX_WEAPON"
	case uint16(CmdPacketUnlimitedPhoenixWeapon):
		return "ENUM_CMDPACKET_UNLIMITED_PHOENIX_WEAPON"
	case uint16(CmdPacketGetGameInfoGameOfDanjin):
		return "ENUM_CMDPACKET_GET_GAME_INFO_GAME_OF_DANJIN"
	case uint16(CmdPacketRollDiceGameOfDanjin):
		return "ENUM_CMDPACKET_ROLL_DICE_GAME_OF_DANJIN"
	case uint16(CmdPacketResetBoardGameOfDanjin):
		return "ENUM_CMDPACKET_RESET_BOARD_GAME_OF_DANJIN"
	case uint16(CmdPacketForceBuyItemDiceGameOfDanjin):
		return "ENUM_CMDPACKET_FORCE_BUY_ITEM_DICE_GAME_OF_DANJIN"
	case uint16(CmdPacketRenewalHotTimeEvent):
		return "ENUM_CMDPACKET_RENEWAL_HOT_TIME_EVENT"
	case uint16(CmdPacketLudmillaSupport):
		return "ENUM_CMDPACKET_LUDMILLA_SUPPORT"
	case uint16(CmdPacketRequestRewardSecondAwakeningEventUniv):
		return "ENUM_CMDPACKET_REQUEST_REWARD_SECOND_AWAKENING_EVENT_UNIV"
	case uint16(CmdPacketGetRewardInfoSecondAwakeningEventUniv):
		return "ENUM_CMDPACKET_GET_REWARD_INFO_SECOND_AWAKENING_EVENT_UNIV"
	case uint16(CmdPacketAboutHope):
		return "ENUM_CMDPACKET_ABOUT_HOPE"
	case uint16(CmdPacketMysteriousGrace):
		return "ENUM_CMDPACKET_MYSTERIOUS_GRACE"
	case uint16(CmdPacketLeaveInNationalday):
		return "ENUM_CMDPACKET_LEAVE_IN_NATIONALDAY"
	case uint16(CmdPacketNationalDay2015):
		return "ENUM_CMDPACKET_NATIONAL_DAY_2015"
	case uint16(CmdPacketEventPVPAccount):
		return "ENUM_CMDPACKET_EVENT_PVP_ACCOUNT"
	case uint16(CmdPacketClearComboWithProleague):
		return "ENUM_CMDPACKET_CLEAR_COMBO_WITH_PROLEAGUE"
	case uint16(CmdPacketDailyCharacDungoneClearJpn):
		return "ENUM_CMDPACKET_DAILY_CHARAC_DUNGONE_CLEAR_JPN"
	case uint16(CmdPacketFighterSkillEventRewardJpn):
		return "ENUM_CMDPACKET_FIGHTER_SKILL_EVENT_REWARD_JPN"
	case uint16(CmdPacketFighterSkillEventUseSkillJpn):
		return "ENUM_CMDPACKET_FIGHTER_SKILL_EVENT_USE_SKILL_JPN"
	case uint16(CmdPacketItemPickupEventJpn):
		return "ENUM_CMDPACKET_ITEM_PICKUP_EVENT_JPN"
	case uint16(CmdPacketCrackOfDimmensionRewardJpn):
		return "ENUM_CMDPACKET_CRACK_OF_DIMMENSION_REWARD_JPN"
	case uint16(CmdPacketHitHugePumpkinInfo):
		return "ENUM_CMDPACKET_HIT_HUGE_PUMPKIN_INFO"
	case uint16(CmdPacketHitHugePumpkinUseAx):
		return "ENUM_CMDPACKET_HIT_HUGE_PUMPKIN_USE_AX"
	case uint16(CmdPacketHotDeal):
		return "ENUM_CMDPACKET_HOT_DEAL"
	case uint16(CmdPacketSoloday2ndLike):
		return "ENUM_CMDPACKET_SOLODAY_2ND_LIKE"
	case uint16(CmdPacketGrowthWeaponRequest):
		return "ENUM_CMDPACKET_GROWTH_WEAPON_REQUEST"
	case uint16(CmdPacketColosseumSeason3RequestGaraponJpn):
		return "ENUM_CMDPACKET_COLOSSEUM_SEASON3_REQUEST_GARAPON_JPN"
	case uint16(CmdPacketNeosPremiumContractRentItem):
		return "ENUM_CMDPACKET_NEOS_PREMIUM_CONTRACT_RENT_ITEM"
	case uint16(CmdPacketNeosPremiumContractRequestGiftItem):
		return "ENUM_CMDPACKET_NEOS_PREMIUM_CONTRACT_REQUEST_GIFT_ITEM"
	case uint16(CmdPacketDanjinSecretCoinReduxEventUniv):
		return "ENUM_CMDPACKET_DANJIN_SECRET_COIN_REDUX_EVENT_UNIV"
	case uint16(CmdPacketNewEveryDayDfo):
		return "ENUM_CMDPACKET_NEW_EVERY_DAY_DFO"
	case uint16(CmdPacketTechIndexImgResourceOptimizeUniv):
		return "ENUM_CMDPACKET_TECH_INDEX_IMG_RESOURCE_OPTIMIZE_UNIV"
	case uint16(CmdPacketTrickOrTreatEventUniv):
		return "ENUM_CMDPACKET_TRICK_OR_TREAT_EVENT_UNIV"
	case uint16(CmdPacketOnePlusOneNotTwoEventUniv):
		return "ENUM_CMDPACKET_ONE_PLUS_ONE_NOT_TWO_EVENT_UNIV"
	case uint16(CmdPacketArachiEventChangeStateJpn):
		return "ENUM_CMDPACKET_ARACHI_EVENT_CHANGE_STATE_JPN"
	case uint16(CmdPacketArachiEventActionJpn):
		return "ENUM_CMDPACKET_ARACHI_EVENT_ACTION_JPN"
	case uint16(CmdPacketMissionEventUpdateJpn):
		return "ENUM_CMDPACKET_MISSION_EVENT_UPDATE_JPN"
	case uint16(CmdPacketMissionEventRewardJpn):
		return "ENUM_CMDPACKET_MISSION_EVENT_REWARD_JPN"
	case uint16(CmdPacketInvitationOfShusiaReward):
		return "ENUM_CMDPACKET_INVITATION_OF_SHUSIA_REWARD"
	case uint16(CmdPacketInvitationOfShusiaMissionComplete):
		return "ENUM_CMDPACKET_INVITATION_OF_SHUSIA_MISSION_COMPLETE"
	case uint16(CmdPacketTerritoryCombatCondition):
		return "ENUM_CMDPACKET_TERRITORY_COMBAT_CONDITION"
	case uint16(CmdPacketCeraGetOrSavingChoice):
		return "ENUM_CMDPACKET_CERA_GET_OR_SAVING_CHOICE"
	case uint16(CmdPacketEventPacketJpn):
		return "ENUM_CMDPACKET_EVENT_PACKET_JPN"
	case uint16(CmdPacketEverydayBossTower):
		return "ENUM_CMDPACKET_EVERYDAY_BOSS_TOWER"
	case uint16(CmdPacketF1PVPAfterReward):
		return "ENUM_CMDPACKET_F1_PVP_AFTER_REWARD"
	case uint16(CmdPacketFishingEventAttendance):
		return "ENUM_CMDPACKET_FISHING_EVENT_ATTENDANCE"
	case uint16(CmdPacketTurnOnTheLuckyLamp):
		return "ENUM_CMDPACKET_TURN_ON_THE_LUCKY_LAMP"
	case uint16(CmdPacketGiftOfSeria):
		return "ENUM_CMDPACKET_GIFT_OF_SERIA"
	case uint16(CmdPacketLetheContract2015):
		return "ENUM_CMDPACKET_LETHE_CONTRACT_2015"
	case uint16(CmdPacketYundyEvent):
		return "ENUM_CMDPACKET_YUNDY_EVENT"
	case uint16(CmdPacketBigTreesmasEventUniv):
		return "ENUM_CMDPACKET_BIG_TREESMAS_EVENT_UNIV"
	case uint16(CmdPacketDespairTowerEventUniv):
		return "ENUM_CMDPACKET_DESPAIR_TOWER_EVENT_UNIV"
	case uint16(CmdPacketOnYourMarkEventUniv):
		return "ENUM_CMDPACKET_ON_YOUR_MARK_EVENT_UNIV"
	case uint16(CmdPacketAreYouReadyEventUniv):
		return "ENUM_CMDPACKET_ARE_YOU_READY_EVENT_UNIV"
	case uint16(CmdPacketCharacterDayEventUniv):
		return "ENUM_CMDPACKET_CHARACTER_DAY_EVENT_UNIV"
	case uint16(CmdPacketPCRoomServiceReqUniv):
		return "ENUM_CMDPACKET_PC_ROOM_SERVICE_REQ_UNIV"
	case uint16(CmdPacketValentineDayEventUniv):
		return "ENUM_CMDPACKET_VALENTINE_DAY_EVENT_UNIV"
	case uint16(CmdPacketCardGame):
		return "ENUM_CMDPACKET_CARD_GAME"
	case uint16(CmdPacketCardGameCompound):
		return "ENUM_CMDPACKET_CARD_GAME_COMPOUND"
	case uint16(CmdPacketGuildModeChangeChn):
		return "ENUM_CMDPACKET_GUILD_MODE_CHANGE_CHN"
	case uint16(CmdPacketCreatureEffectTimeExpire):
		return "ENUM_CMDPACKET_CREATURE_EFFECT_TIME_EXPIRE"
	case uint16(CmdPacketMirrorAradEventReqUniv):
		return "ENUM_CMDPACKET_MIRROR_ARAD_EVENT_REQ_UNIV"
	case uint16(CmdPacketDnfSchool):
		return "ENUM_CMDPACKET_DNF_SCHOOL"
	case uint16(CmdPacketRequestLaundry):
		return "ENUM_CMDPACKET_REQUEST_LAUNDRY"
	case uint16(CmdPacketFriendshipHellPartySelect):
		return "ENUM_CMDPACKET_FRIENDSHIP_HELL_PARTY_SELECT"
	case uint16(CmdPacketContractOfGuild):
		return "ENUM_CMDPACKET_CONTRACT_OF_GUILD"
	case uint16(CmdPacketGrowBeanstalkNPC):
		return "ENUM_CMDPACKET_GROW_BEANSTALK_NPC"
	case uint16(CmdPacketNPCGrowUpUseItem):
		return "ENUM_CMDPACKET_NPC_GROW_UP_USE_ITEM"
	case uint16(CmdPacketP1Tournament):
		return "ENUM_CMDPACKET_P1_TOURNAMENT"
	case uint16(CmdPacketSercretCoinRouletteAddChanceUniv):
		return "ENUM_CMDPACKET_SERCRET_COIN_ROULETTE_ADD_CHANCE_UNIV"
	case uint16(CmdPacketSercretCoinRoulettePlayUniv):
		return "ENUM_CMDPACKET_SERCRET_COIN_ROULETTE_PLAY_UNIV"
	case uint16(CmdPacketChoiceRoulette):
		return "ENUM_CMDPACKET_CHOICE_ROULETTE"
	case uint16(CmdPacketNewbieAndRetureneeBonusReward):
		return "ENUM_CMDPACKET_NEWBIE_AND_RETURENEE_BONUS_REWARD"
	case uint16(CmdPacketHalidomRentalReqUniv):
		return "ENUM_CMDPACKET_HALIDOM_RENTAL_REQ_UNIV"
	case uint16(CmdPacketDnfDraftReqUniv):
		return "ENUM_CMDPACKET_DNF_DRAFT_REQ_UNIV"
	case uint16(CmdPacketDnfDraftResponseUniv):
		return "ENUM_CMDPACKET_DNF_DRAFT_RESPONSE_UNIV"
	case uint16(CmdPacketDnfDraftShopPurchaseUniv):
		return "ENUM_CMDPACKET_DNF_DRAFT_SHOP_PURCHASE_UNIV"
	case uint16(CmdPacketDnfDraftTargetStateUniv):
		return "ENUM_CMDPACKET_DNF_DRAFT_TARGET_STATE_UNIV"
	case uint16(CmdPacketDnfDraftRecommendStateUniv):
		return "ENUM_CMDPACKET_DNF_DRAFT_RECOMMEND_STATE_UNIV"
	case uint16(CmdPacketAppointedDungeonClear):
		return "ENUM_CMDPACKET_APPOINTED_DUNGEON_CLEAR"
	case uint16(CmdPacketRealEstateUseShieldItem):
		return "ENUM_CMDPACKET_REAL_ESTATE_USE_SHIELD_ITEM"
	case uint16(CmdPacketGuessNumber):
		return "ENUM_CMDPACKET_GUESS_NUMBER"
	case uint16(CmdPacketFirstLoginRewardPopup):
		return "ENUM_CMDPACKET_FIRST_LOGIN_REWARD_POPUP"
	case uint16(CmdPacketLogConnectProcess):
		return "ENUM_CMDPACKET_LOG_CONNECT_PROCESS"
	case uint16(CmdPacketSakuraEvent2016):
		return "ENUM_CMDPACKET_SAKURA_EVENT_2016"
	case uint16(CmdPacketLuckyBalloon):
		return "ENUM_CMDPACKET_LUCKY_BALLOON"
	case uint16(CmdPacketFoodFighterDungeon):
		return "ENUM_CMDPACKET_FOOD_FIGHTER_DUNGEON"
	case uint16(CmdPacketArcadePVPDataCopy):
		return "ENUM_CMDPACKET_ARCADE_PVP_DATA_COPY"
	case uint16(CmdPacketPonguntookukReward):
		return "ENUM_CMDPACKET_PONGUNTOOKUK_REWARD"
	case uint16(CmdPacketApcPVPStart):
		return "ENUM_CMDPACKET_APC_PVP_START"
	case uint16(CmdPacketApcPVPDie):
		return "ENUM_CMDPACKET_APC_PVP_DIE"
	case uint16(CmdPacketApcPVPTimeOut):
		return "ENUM_CMDPACKET_APC_PVP_TIME_OUT"
	case uint16(CmdPacketUsagePVPFatigue):
		return "ENUM_CMDPACKET_USAGE_PVP_FATIGUE"
	case uint16(CmdPacketPVPFatigueReward):
		return "ENUM_CMDPACKET_PVP_FATIGUE_REWARD"
	case uint16(CmdPacketChangePVPPrivateToNomal):
		return "ENUM_CMDPACKET_CHANGE_PVP_PRIVATE_TO_NOMAL"
	case uint16(CmdPacketEventMissionForGuildUniv):
		return "ENUM_CMDPACKET_EVENT_MISSION_FOR_GUILD_UNIV"
	case uint16(CmdPacketDnfWithFriendsCharacInfo):
		return "ENUM_CMDPACKET_DNF_WITH_FRIENDS_CHARAC_INFO"
	case uint16(CmdPacketCheckUserConnection):
		return "ENUM_CMDPACKET_CHECK_USER_CONNECTION"
	case uint16(CmdPacketYundyRunGiveRewardUniv):
		return "ENUM_CMDPACKET_YUNDY_RUN_GIVE_REWARD_UNIV"
	case uint16(CmdPacketSnorkelingInfo):
		return "ENUM_CMDPACKET_SNORKELING_INFO"
	case uint16(CmdPacketRavenBridge):
		return "ENUM_CMDPACKET_RAVEN_BRIDGE"
	case uint16(CmdPacketEventLuckySevenUniv):
		return "ENUM_CMDPACKET_EVENT_LUCKY_SEVEN_UNIV"
	case uint16(CmdPacketEventDarkElfDungeonUniv):
		return "ENUM_CMDPACKET_EVENT_DARK_ELF_DUNGEON_UNIV"
	case uint16(CmdPacketPotionologyComposeUniv):
		return "ENUM_CMDPACKET_POTIONOLOGY_COMPOSE_UNIV"
	case uint16(CmdPacketPotionologyTryAnswerUniv):
		return "ENUM_CMDPACKET_POTIONOLOGY_TRY_ANSWER_UNIV"
	case uint16(CmdPacketSteamRequestDlcPackageItem):
		return "ENUM_CMDPACKET_STEAM_REQUEST_DLC_PACKAGE_ITEM"
	case uint16(CmdPacketArcademodePVPRoundInfo):
		return "ENUM_CMDPACKET_ARCADEMODE_PVP_ROUND_INFO"
	case uint16(CmdPacketTextureMemoryStatistics):
		return "ENUM_CMDPACKET_TEXTURE_MEMORY_STATISTICS"
	case uint16(CmdPacketDungeonTextureMemoryStatistics):
		return "ENUM_CMDPACKET_DUNGEON_TEXTURE_MEMORY_STATISTICS"
	case uint16(CmdPacketSelectDamageFontSkin):
		return "ENUM_CMDPACKET_SELECT_DAMAGE_FONT_SKIN"
	case uint16(CmdPacketSaveDnfPremierLeagueRecord):
		return "ENUM_CMDPACKET_SAVE_DNF_PREMIER_LEAGUE_RECORD"
	case uint16(CmdPacketCircusDungeonReward):
		return "ENUM_CMDPACKET_CIRCUS_DUNGEON_REWARD"
	case uint16(CmdPacketJoustInfo):
		return "ENUM_CMDPACKET_JOUST_INFO"
	case uint16(CmdPacketJoustBetting):
		return "ENUM_CMDPACKET_JOUST_BETTING"
	case uint16(CmdPacketJoustMatchHistory):
		return "ENUM_CMDPACKET_JOUST_MATCH_HISTORY"
	case uint16(CmdPacketAtDailyAttendance):
		return "ENUM_CMDPACKET_AT_DAILY_ATTENDANCE"
	case uint16(CmdPacketAveragePingLogChn):
		return "ENUM_CMDPACKET_AVERAGE_PING_LOG_CHN"
	case uint16(CmdPacketWarPreparationEventUniv):
		return "ENUM_CMDPACKET_WAR_PREPARATION_EVENT_UNIV"
	case uint16(CmdPacketAntonRaidEventBuff):
		return "ENUM_CMDPACKET_ANTON_RAID_EVENT_BUFF"
	case uint16(CmdPacketFishingEventUniv):
		return "ENUM_CMDPACKET_FISHING_EVENT_UNIV"
	case uint16(CmdPacketHalloween2016Chn):
		return "ENUM_CMDPACKET_HALLOWEEN_2016_CHN"
	case uint16(CmdPacketGetGuildHongbaoList):
		return "ENUM_CMDPACKET_GET_GUILD_HONGBAO_LIST"
	case uint16(CmdPacketGetGuildHongbaoPointList):
		return "ENUM_CMDPACKET_GET_GUILD_HONGBAO_POINT_LIST"
	case uint16(CmdPacketGetGuildHongbaoHistoryList):
		return "ENUM_CMDPACKET_GET_GUILD_HONGBAO_HISTORY_LIST"
	case uint16(CmdPacketGiveGuildHongbao):
		return "ENUM_CMDPACKET_GIVE_GUILD_HONGBAO"
	case uint16(CmdPacketTakeGuildHongbao):
		return "ENUM_CMDPACKET_TAKE_GUILD_HONGBAO"
	case uint16(CmdPacketReqRewardGuildSpecEvent):
		return "ENUM_CMDPACKET_REQ_REWARD_GUILD_SPEC_EVENT"
	case uint16(CmdPacketReqGuildSpecInfo):
		return "ENUM_CMDPACKET_REQ_GUILD_SPEC_INFO"
	case uint16(CmdPacketMoveToVillagePrev):
		return "ENUM_CMDPACKET_MOVE_TO_VILLAGE_PREV"
	case uint16(CmdPacketUserCheckstatDistribution):
		return "ENUM_CMDPACKET_USER_CHECKSTAT_DISTRIBUTION"
	case uint16(CmdPacketRegisterFreeCashAccount):
		return "ENUM_CMDPACKET_REGISTER_FREE_CASH_ACCOUNT"
	case uint16(CmdPacketFreeCashRewardRemainCount):
		return "ENUM_CMDPACKET_FREE_CASH_REWARD_REMAIN_COUNT"
	case uint16(CmdPacketRequestFreeCashReward):
		return "ENUM_CMDPACKET_REQUEST_FREE_CASH_REWARD"
	case uint16(CmdPacketCircusDungeonUniv):
		return "ENUM_CMDPACKET_CIRCUS_DUNGEON_UNIV"
	case uint16(CmdPacketRequestGaraponOpenJpn):
		return "ENUM_CMDPACKET_REQUEST_GARAPON_OPEN_JPN"
	case uint16(CmdPacketCharacBuffDayEventUniv):
		return "ENUM_CMDPACKET_CHARAC_BUFF_DAY_EVENT_UNIV"
	case uint16(CmdPacketAvatarConvertWitchesPot):
		return "ENUM_CMDPACKET_AVATAR_CONVERT_WITCHES_POT"
	case uint16(CmdPacketDoubleupMinigame):
		return "ENUM_CMDPACKET_DOUBLEUP_MINIGAME"
	case uint16(CmdPacketChronicleFullSetEventUniv):
		return "ENUM_CMDPACKET_CHRONICLE_FULL_SET_EVENT_UNIV"
	case uint16(CmdPacketPackagebonusSeasonserverUniv):
		return "ENUM_CMDPACKET_PACKAGEBONUS_SEASONSERVER_UNIV"
	case uint16(CmdPacketErrorImageListStat):
		return "ENUM_CMDPACKET_ERROR_IMAGE_LIST_STAT"
	case uint16(CmdPacketLogDomainConnect):
		return "ENUM_CMDPACKET_LOG_DOMAIN_CONNECT"
	case uint16(CmdPacketJewelryBattleStart):
		return "ENUM_CMDPACKET_JEWELRY_BATTLE_START"
	case uint16(CmdPacketJewelryBattleJewelryCheck):
		return "ENUM_CMDPACKET_JEWELRY_BATTLE_JEWELRY_CHECK"
	case uint16(CmdPacketRequestOfIllusion):
		return "ENUM_CMDPACKET_REQUEST_OF_ILLUSION"
	case uint16(CmdPacketFriendRecommendSetRewardCharac):
		return "ENUM_CMDPACKET_FRIEND_RECOMMEND_SET_REWARD_CHARAC"
	case uint16(CmdPacketLetsPlayDfoEventUniv):
		return "ENUM_CMDPACKET_LETS_PLAY_DFO_EVENT_UNIV"
	case uint16(CmdPacketRobinRaid):
		return "ENUM_CMDPACKET_ROBIN_RAID"
	case uint16(CmdPacketDetectiveDungeonPuzzle):
		return "ENUM_CMDPACKET_DETECTIVE_DUNGEON_PUZZLE"
	case uint16(CmdPacketAradDetectiveOffice):
		return "ENUM_CMDPACKET_ARAD_DETECTIVE_OFFICE"
	case uint16(CmdPacketJoanMagicalLampRequest):
		return "ENUM_CMDPACKET_JOAN_MAGICAL_LAMP_REQUEST"
	case uint16(CmdPacketWishLanterns):
		return "ENUM_CMDPACKET_WISH_LANTERNS"
	case uint16(CmdPacketAtPriest2Awakening):
		return "ENUM_CMDPACKET_AT_PRIEST_2_AWAKENING"
	case uint16(CmdPacketSupportEkern):
		return "ENUM_CMDPACKET_SUPPORT_EKERN"
	case uint16(CmdPacketUpdateHangSocksCountUniv):
		return "ENUM_CMDPACKET_UPDATE_HANG_SOCKS_COUNT_UNIV"
	case uint16(CmdPacketSetHangSocksUniv):
		return "ENUM_CMDPACKET_SET_HANG_SOCKS_UNIV"
	case uint16(CmdPacketHangSocksRewardUniv):
		return "ENUM_CMDPACKET_HANG_SOCKS_REWARD_UNIV"
	case uint16(CmdPacketDailyRewardUniv):
		return "ENUM_CMDPACKET_DAILY_REWARD_UNIV"
	case uint16(CmdPacketBeginning2017EventUniv):
		return "ENUM_CMDPACKET_BEGINNING_2017_EVENT_UNIV"
	case uint16(CmdPacketBurningPVPEventUniv):
		return "ENUM_CMDPACKET_BURNING_PVP_EVENT_UNIV"
	case uint16(CmdPacketNeopremiumReformEventUniv):
		return "ENUM_CMDPACKET_NEOPREMIUM_REFORM_EVENT_UNIV"
	case uint16(CmdPacketCustomAbilityEquipOption):
		return "ENUM_CMDPACKET_CUSTOM_ABILITY_EQUIP_OPTION"
	case uint16(CmdPacketCustomAbilitySetEquipOption):
		return "ENUM_CMDPACKET_CUSTOM_ABILITY_SET_EQUIP_OPTION"
	case uint16(CmdPacketCustomAbilityUpgrade):
		return "ENUM_CMDPACKET_CUSTOM_ABILITY_UPGRADE"
	case uint16(CmdPacketLogAbnormalDamage):
		return "ENUM_CMDPACKET_LOG_ABNORMAL_DAMAGE"
	case uint16(CmdPacketEquipmentMaskingCharacInfo):
		return "ENUM_CMDPACKET_EQUIPMENT_MASKING_CHARAC_INFO"
	case uint16(CmdPacketAgitWarInfo):
		return "ENUM_CMDPACKET_AGIT_WAR_INFO"
	case uint16(CmdPacketAgitWarMissionReward):
		return "ENUM_CMDPACKET_AGIT_WAR_MISSION_REWARD"
	case uint16(CmdPacketVoiceChatMemberInit):
		return "ENUM_CMDPACKET_VOICE_CHAT_MEMBER_INIT"
	case uint16(CmdPacketVoiceChatCreateRoom):
		return "ENUM_CMDPACKET_VOICE_CHAT_CREATE_ROOM"
	case uint16(CmdPacketVoiceChatRoomList):
		return "ENUM_CMDPACKET_VOICE_CHAT_ROOM_LIST"
	case uint16(CmdPacketAgitWarGuardian):
		return "ENUM_CMDPACKET_AGIT_WAR_GUARDIAN"
	case uint16(CmdPacketAgitWarExtend):
		return "ENUM_CMDPACKET_AGIT_WAR_EXTEND"
	case uint16(CmdPacketUpdateWishItem):
		return "ENUM_CMDPACKET_UPDATE_WISH_ITEM"
	case uint16(CmdPacketEggWatchPhaseup):
		return "ENUM_CMDPACKET_EGG_WATCH_PHASEUP"
	case uint16(CmdPacketAgitWarShop):
		return "ENUM_CMDPACKET_AGIT_WAR_SHOP"
	case uint16(CmdPacketAgitWarDungeonRequirePoint):
		return "ENUM_CMDPACKET_AGIT_WAR_DUNGEON_REQUIRE_POINT"
	case uint16(CmdPacketRequestDungeonDriverCharacter):
		return "ENUM_CMDPACKET_REQUEST_DUNGEON_DRIVER_CHARACTER"
	case uint16(CmdPacketDecideDungeonDriverCharacter):
		return "ENUM_CMDPACKET_DECIDE_DUNGEON_DRIVER_CHARACTER"
	case uint16(CmdPacketFramelagPerPVP):
		return "ENUM_CMDPACKET_FRAMELAG_PER_PVP"
	case uint16(CmdPacketRequestRaidInfo):
		return "ENUM_CMDPACKET_REQUEST_RAID_INFO"
	case uint16(CmdPacketAgitWarSeasonReward):
		return "ENUM_CMDPACKET_AGIT_WAR_SEASON_REWARD"
	case uint16(CmdPacketUDPPacketNetworkStatisticPerSec):
		return "ENUM_CMDPACKET_UDP_PACKET_NETWORK_STATISTIC_PER_SEC"
	case uint16(CmdPacketUDPPacketStatData):
		return "ENUM_CMDPACKET_UDP_PACKET_STAT_DATA"
	case uint16(CmdPacketUDPPacketPingPerSize):
		return "ENUM_CMDPACKET_UDP_PACKET_PING_PER_SIZE"
	case uint16(CmdPacketAgitWarSelectChallengeGuild):
		return "ENUM_CMDPACKET_AGIT_WAR_SELECT_CHALLENGE_GUILD"
	case uint16(CmdPacketTcpPacketStatData):
		return "ENUM_CMDPACKET_TCP_PACKET_STAT_DATA"
	case uint16(CmdPacketSsdUtilizationRate):
		return "ENUM_CMDPACKET_SSD_UTILIZATION_RATE"
	case uint16(CmdPacketRequestRaidEntranceInfo):
		return "ENUM_CMDPACKET_REQUEST_RAID_ENTRANCE_INFO"
	case uint16(CmdPacketStartGentResistance):
		return "ENUM_CMDPACKET_START_GENT_RESISTANCE"
	case uint16(CmdPacketEndGentResistance):
		return "ENUM_CMDPACKET_END_GENT_RESISTANCE"
	case uint16(CmdPacketRequestWeekendTimeBonus):
		return "ENUM_CMDPACKET_REQUEST_WEEKEND_TIME_BONUS"
	case uint16(CmdPacketUpdateMoonlightTavernSystem):
		return "ENUM_CMDPACKET_UPDATE_MOONLIGHT_TAVERN_SYSTEM"
	case uint16(CmdPacketMoonlightTavernMessage):
		return "ENUM_CMDPACKET_MOONLIGHT_TAVERN_MESSAGE"
	case uint16(CmdPacketStartChainRush):
		return "ENUM_CMDPACKET_START_CHAIN_RUSH"
	case uint16(CmdPacketStopChainRush):
		return "ENUM_CMDPACKET_STOP_CHAIN_RUSH"
	case uint16(CmdPacketSizukiArenaSeason2EventUniv):
		return "ENUM_CMDPACKET_SIZUKI_ARENA_SEASON2_EVENT_UNIV"
	case uint16(CmdPacketChocolatierEventMakeReqUniv):
		return "ENUM_CMDPACKET_CHOCOLATIER_EVENT_MAKE_REQ_UNIV"
	case uint16(CmdPacketChocolatierEventRecvRewardUniv):
		return "ENUM_CMDPACKET_CHOCOLATIER_EVENT_RECV_REWARD_UNIV"
	case uint16(CmdPacketChocolatierEventReqNotiUniv):
		return "ENUM_CMDPACKET_CHOCOLATIER_EVENT_REQ_NOTI_UNIV"
	case uint16(CmdPacketLightDarkEventSpecReward):
		return "ENUM_CMDPACKET_LIGHT_DARK_EVENT_SPEC_REWARD"
	case uint16(CmdPacketLightDarkTurnEnd):
		return "ENUM_CMDPACKET_LIGHT_DARK_TURN_END"
	case uint16(CmdPacketLightDarkSelectCard):
		return "ENUM_CMDPACKET_LIGHT_DARK_SELECT_CARD"
	case uint16(CmdPacketLightDarkTimeOutPower):
		return "ENUM_CMDPACKET_LIGHT_DARK_TIME_OUT_POWER"
	case uint16(CmdPacketNurtureAccEventCharacRewardUniv):
		return "ENUM_CMDPACKET_NURTURE_ACC_EVENT_CHARAC_REWARD_UNIV"
	case uint16(CmdPacketNurtureAccEventRewardUniv):
		return "ENUM_CMDPACKET_NURTURE_ACC_EVENT_REWARD_UNIV"
	case uint16(CmdPacketWeeklyAttendanceReward):
		return "ENUM_CMDPACKET_WEEKLY_ATTENDANCE_REWARD"
	case uint16(CmdPacketDanjinBreakGameStart):
		return "ENUM_CMDPACKET_DANJIN_BREAK_GAME_START"
	case uint16(CmdPacketDanjinBreakReward):
		return "ENUM_CMDPACKET_DANJIN_BREAK_REWARD"
	case uint16(CmdPacketFindingNumberResult):
		return "ENUM_CMDPACKET_FINDING_NUMBER_RESULT"
	case uint16(CmdPacketDailyDungeonReward):
		return "ENUM_CMDPACKET_DAILY_DUNGEON_REWARD"
	case uint16(CmdPacketDailyDungeonMissionChange):
		return "ENUM_CMDPACKET_DAILY_DUNGEON_MISSION_CHANGE"
	case uint16(CmdPacketMonsterCardQuizStart):
		return "ENUM_CMDPACKET_MONSTER_CARD_QUIZ_START"
	case uint16(CmdPacketMonsterCardQuizAnswer):
		return "ENUM_CMDPACKET_MONSTER_CARD_QUIZ_ANSWER"
	case uint16(CmdPacketMonsterCardQuizNext):
		return "ENUM_CMDPACKET_MONSTER_CARD_QUIZ_NEXT"
	case uint16(CmdPacketSeria2017Reward):
		return "ENUM_CMDPACKET_SERIA_2017_REWARD"
	case uint16(CmdPacketGriefTowerComeOverEvent):
		return "ENUM_CMDPACKET_GRIEF_TOWER_COME_OVER_EVENT"
	case uint16(CmdPacketChronicle999Event):
		return "ENUM_CMDPACKET_CHRONICLE_999_EVENT"
	case uint16(CmdPacketChronicleDonateGoldEvent):
		return "ENUM_CMDPACKET_CHRONICLE_DONATE_GOLD_EVENT"
	case uint16(CmdPacketOriginPreludeDialog):
		return "ENUM_CMDPACKET_ORIGIN_PRELUDE_DIALOG"
	case uint16(CmdPacketOriginPreludeReward):
		return "ENUM_CMDPACKET_ORIGIN_PRELUDE_REWARD"
	case uint16(CmdPacketUIHistoryLogChn):
		return "ENUM_CMDPACKET_UI_HISTORY_LOG_CHN"
	case uint16(CmdPacketReqPersonalTrainingCharacter):
		return "ENUM_CMDPACKET_REQ_PERSONAL_TRAINING_CHARACTER"
	case uint16(CmdPacketPartyCorpseHitRenewal):
		return "ENUM_CMDPACKET_PARTY_CORPSE_HIT_RENEWAL"
	case uint16(CmdPacketRequestAdventureInfo):
		return "ENUM_CMDPACKET_REQUEST_ADVENTURE_INFO"
	case uint16(CmdPacketRemotePeerPacket):
		return "ENUM_CMDPACKET_REMOTE_PEER_PACKET"
	case uint16(CmdPacketReqRemotePeer):
		return "ENUM_CMDPACKET_REQ_REMOTE_PEER"
	case uint16(CmdPacketResRemotePeer):
		return "ENUM_CMDPACKET_RES_REMOTE_PEER"
	case uint16(CmdPacketMercenaryCompetitionCancle):
		return "ENUM_CMDPACKET_MERCENARY_COMPETITION_CANCLE"
	case uint16(CmdPacketMercenaryCompetitionRewardRequest):
		return "ENUM_CMDPACKET_MERCENARY_COMPETITION_REWARD_REQUEST"
	case uint16(CmdPacketMercenaryPointRecalculate):
		return "ENUM_CMDPACKET_MERCENARY_POINT_RECALCULATE"
	case uint16(CmdPacketMomentLagStatistic):
		return "ENUM_CMDPACKET_MOMENT_LAG_STATISTIC"
	case uint16(CmdPacketBetrayalDungeonAnswer):
		return "ENUM_CMDPACKET_BETRAYAL_DUNGEON_ANSWER"
	case uint16(CmdPacketRequestPcroomNexonCashEventInfo):
		return "ENUM_CMDPACKET_REQUEST_PCROOM_NEXON_CASH_EVENT_INFO"
	case uint16(CmdPacketRequestDailyGift):
		return "ENUM_CMDPACKET_REQUEST_DAILY_GIFT"
	case uint16(CmdPacketAdventurerShopPurchase):
		return "ENUM_CMDPACKET_ADVENTURER_SHOP_PURCHASE"
	case uint16(CmdPacketChildrensdayGiftShootingDelivery):
		return "ENUM_CMDPACKET_CHILDRENSDAY_GIFT_SHOOTING_DELIVERY"
	case uint16(CmdPacketWarriorMaker):
		return "ENUM_CMDPACKET_WARRIOR_MAKER"
	case uint16(CmdPacketEpicProductionStartFinish):
		return "ENUM_CMDPACKET_EPIC_PRODUCTION_START_FINISH"
	case uint16(CmdPacketEpicProductionChangeItem):
		return "ENUM_CMDPACKET_EPIC_PRODUCTION_CHANGE_ITEM"
	case uint16(CmdPacketEpicProductionProcess):
		return "ENUM_CMDPACKET_EPIC_PRODUCTION_PROCESS"
	case uint16(CmdPacketEpicProductionMaterialCompound):
		return "ENUM_CMDPACKET_EPIC_PRODUCTION_MATERIAL_COMPOUND"
	case uint16(CmdPacketEpicProductionAbilityOption):
		return "ENUM_CMDPACKET_EPIC_PRODUCTION_ABILITY_OPTION"
	case uint16(CmdPacketAutoRegisterEventCharacter):
		return "ENUM_CMDPACKET_AUTO_REGISTER_EVENT_CHARACTER"
	case uint16(CmdPacketSummer2017):
		return "ENUM_CMDPACKET_SUMMER_2017"
	case uint16(CmdPacketDailyAttendanceCheckReq):
		return "ENUM_CMDPACKET_DAILY_ATTENDANCE_CHECK_REQ"
	case uint16(CmdPacketPrevVillage):
		return "ENUM_CMDPACKET_PREV_VILLAGE"
	case uint16(CmdPacketFpsDevideCollect):
		return "ENUM_CMDPACKET_FPS_DEVIDE_COLLECT"
	case uint16(CmdPacketRequestCardPick):
		return "ENUM_CMDPACKET_REQUEST_CARD_PICK"
	case uint16(CmdPacketSkillSwitchInventory):
		return "ENUM_CMDPACKET_SKILL_SWITCH_INVENTORY"
	case uint16(CmdPacketClearQuestTicket):
		return "ENUM_CMDPACKET_CLEAR_QUEST_TICKET"
	case uint16(CmdPacketClearBranchQuest):
		return "ENUM_CMDPACKET_CLEAR_BRANCH_QUEST"
	case uint16(CmdPacketNewbieGuideOption):
		return "ENUM_CMDPACKET_NEWBIE_GUIDE_OPTION"
	case uint16(CmdPacketNewbieMissionReward):
		return "ENUM_CMDPACKET_NEWBIE_MISSION_REWARD"
	case uint16(CmdPacketSelectCardSkip):
		return "ENUM_CMDPACKET_SELECT_CARD_SKIP"
	case uint16(CmdPacketRequestOriginReturnUserReward):
		return "ENUM_CMDPACKET_REQUEST_ORIGIN_RETURN_USER_REWARD"
	case uint16(CmdPacketRecommendOriginReturnUser):
		return "ENUM_CMDPACKET_RECOMMEND_ORIGIN_RETURN_USER"
	case uint16(CmdPacketRequestOriginRecommendReward):
		return "ENUM_CMDPACKET_REQUEST_ORIGIN_RECOMMEND_REWARD"
	case uint16(CmdPacketUpdateRepresentAccountName):
		return "ENUM_CMDPACKET_UPDATE_REPRESENT_ACCOUNT_NAME"
	case uint16(CmdPacketAccountFriendAddRequest):
		return "ENUM_CMDPACKET_ACCOUNT_FRIEND_ADD_REQUEST"
	case uint16(CmdPacketAccountFriendAccept):
		return "ENUM_CMDPACKET_ACCOUNT_FRIEND_ACCEPT"
	case uint16(CmdPacketAccountFriendDelete):
		return "ENUM_CMDPACKET_ACCOUNT_FRIEND_DELETE"
	case uint16(CmdPacketAccountFriendRefuseCancel):
		return "ENUM_CMDPACKET_ACCOUNT_FRIEND_REFUSE_CANCEL"
	case uint16(CmdPacketUpdateAccountFriendInfo):
		return "ENUM_CMDPACKET_UPDATE_ACCOUNT_FRIEND_INFO"
	case uint16(CmdPacketRepresentAccountNameDuplicateCheck):
		return "ENUM_CMDPACKET_REPRESENT_ACCOUNT_NAME_DUPLICATE_CHECK"
	case uint16(CmdPacketChangeRepresentAccountName):
		return "ENUM_CMDPACKET_CHANGE_REPRESENT_ACCOUNT_NAME"
	case uint16(CmdPacketStoryDigestUpdate):
		return "ENUM_CMDPACKET_STORY_DIGEST_UPDATE"
	case uint16(CmdPacketSaveRecentEmoticonList):
		return "ENUM_CMDPACKET_SAVE_RECENT_EMOTICON_LIST"
	case uint16(CmdPacketEventBattleshipDungeon):
		return "ENUM_CMDPACKET_EVENT_BATTLESHIP_DUNGEON"
	case uint16(CmdPacketJulySeventhReward):
		return "ENUM_CMDPACKET_JULY_SEVENTH_REWARD"
	case uint16(CmdPacketOneColorBall):
		return "ENUM_CMDPACKET_ONE_COLOR_BALL"
	case uint16(CmdPacketReqGrainCombination):
		return "ENUM_CMDPACKET_REQ_GRAIN_COMBINATION"
	case uint16(CmdPacketNgsSecurityData):
		return "ENUM_CMDPACKET_NGS_SECURITY_DATA"
	case uint16(CmdPacketSearchGuildList):
		return "ENUM_CMDPACKET_SEARCH_GUILD_LIST"
	case uint16(CmdPacketRequestAccountGuildList):
		return "ENUM_CMDPACKET_REQUEST_ACCOUNT_GUILD_LIST"
	case uint16(CmdPacketGuildAllMemberGrade):
		return "ENUM_CMDPACKET_GUILD_ALL_MEMBER_GRADE"
	case uint16(CmdPacketGuildAllMemberGradeNext):
		return "ENUM_CMDPACKET_GUILD_ALL_MEMBER_GRADE_NEXT"
	case uint16(CmdPacketShowroomAvatarRent):
		return "ENUM_CMDPACKET_SHOWROOM_AVATAR_RENT"
	case uint16(CmdPacketQuestReplay):
		return "ENUM_CMDPACKET_QUEST_REPLAY"
	case uint16(CmdPacketUpdateTagTournamentCharacter):
		return "ENUM_CMDPACKET_UPDATE_TAG_TOURNAMENT_CHARACTER"
	case uint16(CmdPacketTagTournamentCharacterTagIn):
		return "ENUM_CMDPACKET_TAG_TOURNAMENT_CHARACTER_TAG_IN"
	case uint16(CmdPacketDieTagTournamentCharacter):
		return "ENUM_CMDPACKET_DIE_TAG_TOURNAMENT_CHARACTER"
	case uint16(CmdPacketDatingSimulation):
		return "ENUM_CMDPACKET_DATING_SIMULATION"
	case uint16(CmdPacketBeLegendEvent):
		return "ENUM_CMDPACKET_BE_LEGEND_EVENT"
	case uint16(CmdPacketBroadcastVoiceChatStatus):
		return "ENUM_CMDPACKET_BROADCAST_VOICE_CHAT_STATUS"
	case uint16(CmdPacketReqAccumulteAttendanceReward):
		return "ENUM_CMDPACKET_REQ_ACCUMULTE_ATTENDANCE_REWARD"
	case uint16(CmdPacketPresentOfAnubyReward):
		return "ENUM_CMDPACKET_PRESENT_OF_ANUBY_REWARD"
	case uint16(CmdPacketGetRepresentCharacJob):
		return "ENUM_CMDPACKET_GET_REPRESENT_CHARAC_JOB"
	case uint16(CmdPacketSetRepresentCharacJob):
		return "ENUM_CMDPACKET_SET_REPRESENT_CHARAC_JOB"
	case uint16(CmdPacketRemoveRepresentCharacJob):
		return "ENUM_CMDPACKET_REMOVE_REPRESENT_CHARAC_JOB"
	case uint16(CmdPacketFakeLoginOpenSpaceSystemTest):
		return "ENUM_CMDPACKET_FAKE_LOGIN_OPEN_SPACE_SYSTEM_TEST"
	case uint16(CmdPacketHitRandombox):
		return "ENUM_CMDPACKET_HIT_RANDOMBOX"
	case uint16(CmdPacketKeepCalmAndRodeoStartGame):
		return "ENUM_CMDPACKET_KEEP_CALM_AND_RODEO_START_GAME"
	case uint16(CmdPacketBangBangBangStartGame):
		return "ENUM_CMDPACKET_BANG_BANG_BANG_START_GAME"
	case uint16(CmdPacketEpicChristmasEvent):
		return "ENUM_CMDPACKET_EPIC_CHRISTMAS_EVENT"
	case uint16(CmdPacketMinority2017Vote):
		return "ENUM_CMDPACKET_MINORITY_2017_VOTE"
	case uint16(CmdPacketMinority2017Info):
		return "ENUM_CMDPACKET_MINORITY_2017_INFO"
	case uint16(CmdPacketMinority2017Reward):
		return "ENUM_CMDPACKET_MINORITY_2017_REWARD"
	case uint16(CmdPacketTurnHellRoulette):
		return "ENUM_CMDPACKET_TURN_HELL_ROULETTE"
	case uint16(CmdPacketCardBattleMoveCard):
		return "ENUM_CMDPACKET_CARD_BATTLE_MOVE_CARD"
	case uint16(CmdPacketCardBattleThrow):
		return "ENUM_CMDPACKET_CARD_BATTLE_THROW"
	case uint16(CmdPacketCardBattleGiveup):
		return "ENUM_CMDPACKET_CARD_BATTLE_GIVEUP"
	case uint16(CmdPacketCardBattleCompound):
		return "ENUM_CMDPACKET_CARD_BATTLE_COMPOUND"
	case uint16(CmdPacketCardBattleAiMode):
		return "ENUM_CMDPACKET_CARD_BATTLE_AI_MODE"
	case uint16(CmdPacketPickAddChanceItem):
		return "ENUM_CMDPACKET_PICK_ADD_CHANCE_ITEM"
	case uint16(CmdPacketEggWatchCureState):
		return "ENUM_CMDPACKET_EGG_WATCH_CURE_STATE"
	case uint16(CmdPacketRedEnvelope):
		return "ENUM_CMDPACKET_RED_ENVELOPE"
	case uint16(CmdPacketRedEnvelopeAccumulateReward):
		return "ENUM_CMDPACKET_RED_ENVELOPE_ACCUMULATE_REWARD"
	case uint16(CmdPacketAvatarFittingRoomChange):
		return "ENUM_CMDPACKET_AVATAR_FITTING_ROOM_CHANGE"
	case uint16(CmdPacketBeastSoul):
		return "ENUM_CMDPACKET_BEAST_SOUL"
	case uint16(CmdPacketReportBeastMonsterHp):
		return "ENUM_CMDPACKET_REPORT_BEAST_MONSTER_HP"
	case uint16(CmdPacketAdventureGrowthcapsuleExp):
		return "ENUM_CMDPACKET_ADVENTURE_GROWTHCAPSULE_EXP"
	case uint16(CmdPacketCheeryBlossomSightseeing):
		return "ENUM_CMDPACKET_CHEERY_BLOSSOM_SIGHTSEEING"
	case uint16(CmdPacketWomensDay):
		return "ENUM_CMDPACKET_WOMENS_DAY"
	case uint16(CmdPacketReqAllservergroupLimitItemCount):
		return "ENUM_CMDPACKET_REQ_ALLSERVERGROUP_LIMIT_ITEM_COUNT"
	case uint16(CmdPacketMermaidStarLiveReward):
		return "ENUM_CMDPACKET_MERMAID_STAR_LIVE_REWARD"
	case uint16(CmdPacketChangeDisguise):
		return "ENUM_CMDPACKET_CHANGE_DISGUISE"
	case uint16(CmdPacketOutsideGameReward):
		return "ENUM_CMDPACKET_OUTSIDE_GAME_REWARD"
	case uint16(CmdPacketHellPartyLiver):
		return "ENUM_CMDPACKET_HELL_PARTY_LIVER"
	case uint16(CmdPacketBattleRoyalInfo):
		return "ENUM_CMDPACKET_BATTLE_ROYAL_INFO"
	case uint16(CmdPacketRequestPlantTree):
		return "ENUM_CMDPACKET_REQUEST_PLANT_TREE"
	case uint16(CmdPacketTwentiethMayValentineDay):
		return "ENUM_CMDPACKET_TWENTIETH_MAY_VALENTINE_DAY"
	case uint16(CmdPacketLabordayPuzzle):
		return "ENUM_CMDPACKET_LABORDAY_PUZZLE"
	case uint16(CmdPacketEventMahjongJpn):
		return "ENUM_CMDPACKET_EVENT_MAHJONG_JPN"
	case uint16(CmdPacketMakingSandwichStart):
		return "ENUM_CMDPACKET_MAKING_SANDWICH_START"
	case uint16(CmdPacketMakingSandwichCheck):
		return "ENUM_CMDPACKET_MAKING_SANDWICH_CHECK"
	case uint16(CmdPacketWhacGameEnd):
		return "ENUM_CMDPACKET_WHAC_GAME_END"
	case uint16(CmdPacketTencentPCRoomLoginReward):
		return "ENUM_CMDPACKET_TENCENT_PC_ROOM_LOGIN_REWARD"
	case uint16(CmdPacketGrantVoiceChat):
		return "ENUM_CMDPACKET_GRANT_VOICE_CHAT"
	case uint16(CmdPacketOldUserFirstLoginRewardPopup):
		return "ENUM_CMDPACKET_OLD_USER_FIRST_LOGIN_REWARD_POPUP"
	case uint16(CmdPacketLetsNewPickPresent):
		return "ENUM_CMDPACKET_LETS_NEW_PICK_PRESENT"
	case uint16(CmdPacketFiendWarBossProc):
		return "ENUM_CMDPACKET_FIEND_WAR_BOSS_PROC"
	case uint16(CmdPacketLionsMinigameStart):
		return "ENUM_CMDPACKET_LIONS_MINIGAME_START"
	case uint16(CmdPacketLionsDinnerBuff):
		return "ENUM_CMDPACKET_LIONS_DINNER_BUFF"
	case uint16(CmdPacketTakeAPictureStep):
		return "ENUM_CMDPACKET_TAKE_A_PICTURE_STEP"
	case uint16(CmdPacketFind7goldBullion):
		return "ENUM_CMDPACKET_FIND_7GOLD_BULLION"
	case uint16(CmdPacketAntibot):
		return "ENUM_CMDPACKET_ANTIBOT"
	case uint16(CmdPacketDproto):
		return "ENUM_CMDPACKET_DPROTO"
	case uint16(CmdPacketDprotoCallback):
		return "ENUM_CMDPACKET_DPROTO_CALLBACK"
	case uint16(CmdPacketEnd):
		return "ENUM_CMDPACKET_END"
	default:
		return "ENUM_CMDPACKET_UNKNOWN"
	}
}

// IsKnownCmdPacket 表示 opcode 是否存在于 runtime 命令名表。
func IsKnownCmdPacket(opcode uint16) bool {
	if opcode > CmdPacketMaxValue {
		return false
	}
	if opcode == 545 {
		return false
	}
	if opcode == 1456 {
		return false
	}
	return true
}
