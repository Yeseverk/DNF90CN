package repository

import "longheng.io/server/internal/platform/db"

var (
	CharacterFields = db.NewFieldRegistry([]CharacterField{
		CharacterFieldBase,
		CharacterFieldStats,
		CharacterFieldLocation,
		CharacterFieldRoster,
	})
	InventoryFields = db.NewFieldRegistry([]InventoryField{
		InventoryFieldSlots,
		InventoryFieldWarehouse,
	})
	EquipmentFields = db.NewFieldRegistry([]EquipmentField{
		EquipmentFieldEntries,
	})
	PetFields = db.NewFieldRegistry([]PetField{
		PetFieldEntries,
		PetFieldEquipped,
		PetFieldDisplay,
	})
	QuestFields = db.NewFieldRegistry([]QuestField{
		QuestFieldStates,
		QuestFieldProgress,
	})
	SkillFields = db.NewFieldRegistry([]SkillField{
		SkillFieldSkills,
		SkillFieldPoints,
		SkillFieldLayouts,
		SkillFieldCooldowns,
	})
	SettingsFields = db.NewFieldRegistry([]SettingsField{
		SettingsFieldValues,
	})
	MailboxFields = db.NewFieldRegistry([]MailboxField{
		MailboxFieldMails,
	})
)

func AllCharacterFields() []CharacterField {
	return CharacterFields.All()
}

func AllInventoryFields() []InventoryField {
	return InventoryFields.All()
}

func AllEquipmentFields() []EquipmentField {
	return EquipmentFields.All()
}

func AllPetFields() []PetField {
	return PetFields.All()
}

func AllQuestFields() []QuestField {
	return QuestFields.All()
}

func AllSkillFields() []SkillField {
	return SkillFields.All()
}

func AllSettingsFields() []SettingsField {
	return SettingsFields.All()
}

func AllMailboxFields() []MailboxField {
	return MailboxFields.All()
}
