package mysql

import (
	"context"
	"database/sql"
	"longheng.io/server/internal/modules/dnf/repository"
)

const mysqlMailboxTable = "mailboxes"

type mysqlMailboxStore struct {
	mysqlStoreBase
}

// Load reads one character mailbox from MySQL.
func (s *mysqlMailboxStore) Load(ctx context.Context, characterID string) (repository.MailboxRecord, bool, error) {
	table, err := s.router.readTable(mysqlMailboxTable, characterID)
	if err != nil {
		return repository.MailboxRecord{}, false, err
	}
	mailsTable, err := s.router.readTable(mysqlMailsTable, characterID)
	if err != nil {
		return repository.MailboxRecord{}, false, err
	}
	metadataTable, err := s.router.readTable(mysqlMailMetadataTable, characterID)
	if err != nil {
		return repository.MailboxRecord{}, false, err
	}
	attachmentsTable, err := s.router.readTable(mysqlMailAttachmentsTable, characterID)
	if err != nil {
		return repository.MailboxRecord{}, false, err
	}
	attachmentExtraTable, err := s.router.readTable(mysqlMailAttachmentExtraTable, characterID)
	if err != nil {
		return repository.MailboxRecord{}, false, err
	}
	query := s.router.selectQuery("SELECT character_id, updated_at FROM " + table + " WHERE character_id = ?")
	var record repository.MailboxRecord
	var updatedAt sql.NullTime
	err = s.router.db.QueryRowContext(ctx, query, characterID).Scan(
		&record.CharacterID,
		&updatedAt,
	)
	if err != nil {
		ok, scanErr := scanErr(err)
		return repository.MailboxRecord{}, ok, scanErr
	}
	record.Mails, err = loadMails(
		ctx,
		s.router,
		mailsTable,
		metadataTable,
		attachmentsTable,
		attachmentExtraTable,
		characterID,
	)
	if err != nil {
		return repository.MailboxRecord{}, false, err
	}
	record.UpdatedAt = scanTime(updatedAt)
	return repository.CloneMailbox(record), true, nil
}

// Save persists the whole mailbox aggregate.
func (s *mysqlMailboxStore) Save(ctx context.Context, record repository.MailboxRecord) error {
	return s.SaveFields(ctx, record, repository.AllMailboxFields()...)
}

// SaveFields persists selected mailbox fields.
func (s *mysqlMailboxStore) SaveFields(ctx context.Context, record repository.MailboxRecord, fields ...repository.MailboxField) error {
	characterID, err := requireRecordKey(repository.MailboxKey, record, "mailbox")
	if err != nil {
		return err
	}
	fields = repository.MailboxFields.Normalize(fields)
	if len(fields) == 0 {
		return nil
	}
	table, err := s.router.writeTable(mysqlMailboxTable, characterID)
	if err != nil {
		return err
	}
	mailsTable, err := s.router.writeTable(mysqlMailsTable, characterID)
	if err != nil {
		return err
	}
	metadataTable, err := s.router.writeTable(mysqlMailMetadataTable, characterID)
	if err != nil {
		return err
	}
	attachmentsTable, err := s.router.writeTable(mysqlMailAttachmentsTable, characterID)
	if err != nil {
		return err
	}
	attachmentExtraTable, err := s.router.writeTable(mysqlMailAttachmentExtraTable, characterID)
	if err != nil {
		return err
	}
	columns := []string{"character_id", "updated_at"}
	args := []any{characterID, timeOrNow(record.UpdatedAt, s.router.now)}
	updates := []string{updateValue("updated_at")}
	saveMails := false
	for _, field := range fields {
		if field == repository.MailboxFieldMails {
			saveMails = true
		}
	}
	return withMySQLWriteExecutor(ctx, s.router.db, func(database SQLDB) error {
		if _, execErr := database.ExecContext(ctx, buildUpsert(table, columns, updates), args...); execErr != nil {
			return execErr
		}
		if !saveMails {
			return nil
		}
		return replaceMails(
			ctx,
			database,
			mailsTable,
			metadataTable,
			attachmentsTable,
			attachmentExtraTable,
			characterID,
			record.Mails,
		)
	})
}

func loadMails(
	ctx context.Context,
	router mysqlRouter,
	mailsTable, metadataTable, attachmentsTable, attachmentExtraTable, characterID string,
) (map[string]repository.MailRecord, error) {
	query := router.selectQuery("SELECT mail_id, sender_character_id, sender_name, recipient_character_id, recipient_name, title, body, gold, read_flag, claimed_flag, deleted_flag, created_at, expire_at FROM " + mailsTable + " WHERE character_id = ? ORDER BY mail_id")
	rows, err := router.db.QueryContext(ctx, query, characterID)
	if err != nil {
		return nil, err
	}
	var mails map[string]repository.MailRecord
	for rows.Next() {
		var mail repository.MailRecord
		var createdAt, expireAt sql.NullTime
		if err := rows.Scan(
			&mail.MailID,
			&mail.SenderCharacterID,
			&mail.SenderName,
			&mail.RecipientCharacterID,
			&mail.RecipientName,
			&mail.Title,
			&mail.Body,
			&mail.Gold,
			&mail.Read,
			&mail.Claimed,
			&mail.Deleted,
			&createdAt,
			&expireAt,
		); err != nil {
			rows.Close()
			return nil, err
		}
		mail.CreatedAt = scanTime(createdAt)
		mail.ExpireAt = scanTime(expireAt)
		if mails == nil {
			mails = make(map[string]repository.MailRecord)
		}
		mails[mail.MailID] = mail
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	metadata, err := router.db.QueryContext(
		ctx,
		router.selectQuery("SELECT mail_id, metadata_key, metadata_value FROM "+metadataTable+" WHERE character_id = ? ORDER BY mail_id, metadata_key"),
		characterID,
	)
	if err != nil {
		return nil, err
	}
	for metadata.Next() {
		var mailID, key, value string
		if err := metadata.Scan(&mailID, &key, &value); err != nil {
			metadata.Close()
			return nil, err
		}
		mail, ok := mails[mailID]
		if !ok {
			continue
		}
		if mail.Metadata == nil {
			mail.Metadata = make(map[string]string)
		}
		mail.Metadata[key] = value
		mails[mailID] = mail
	}
	if err := metadata.Err(); err != nil {
		metadata.Close()
		return nil, err
	}
	if err := metadata.Close(); err != nil {
		return nil, err
	}

	attachments, err := router.db.QueryContext(
		ctx,
		router.selectQuery("SELECT mail_id, attachment_index, item_id, item_count, bind_flag, expire_at, raw_entry FROM "+attachmentsTable+" WHERE character_id = ? ORDER BY mail_id, attachment_index"),
		characterID,
	)
	if err != nil {
		return nil, err
	}
	for attachments.Next() {
		var mailID string
		var index int
		var attachment repository.MailAttachment
		var expireAt sql.NullTime
		var rawEntry []byte
		if err := attachments.Scan(&mailID, &index, &attachment.ItemID, &attachment.Count, &attachment.Bind, &expireAt, &rawEntry); err != nil {
			attachments.Close()
			return nil, err
		}
		mail, ok := mails[mailID]
		if !ok {
			continue
		}
		attachment.ExpireAt = scanTime(expireAt)
		attachment.RawEntry = append([]byte(nil), rawEntry...)
		for len(mail.Attachments) <= index {
			mail.Attachments = append(mail.Attachments, repository.MailAttachment{})
		}
		mail.Attachments[index] = attachment
		mails[mailID] = mail
	}
	if err := attachments.Err(); err != nil {
		attachments.Close()
		return nil, err
	}
	if err := attachments.Close(); err != nil {
		return nil, err
	}

	extras, err := router.db.QueryContext(
		ctx,
		router.selectQuery("SELECT mail_id, attachment_index, extra_key, extra_value FROM "+attachmentExtraTable+" WHERE character_id = ? ORDER BY mail_id, attachment_index, extra_key"),
		characterID,
	)
	if err != nil {
		return nil, err
	}
	defer extras.Close()
	for extras.Next() {
		var mailID, key, value string
		var index int
		if err := extras.Scan(&mailID, &index, &key, &value); err != nil {
			return nil, err
		}
		mail, ok := mails[mailID]
		if !ok || index < 0 || index >= len(mail.Attachments) {
			continue
		}
		if mail.Attachments[index].Extra == nil {
			mail.Attachments[index].Extra = make(map[string]string)
		}
		mail.Attachments[index].Extra[key] = value
		mails[mailID] = mail
	}
	if err := extras.Err(); err != nil {
		return nil, err
	}
	return mails, nil
}

func replaceMails(
	ctx context.Context,
	database SQLDB,
	mailsTable, metadataTable, attachmentsTable, attachmentExtraTable, characterID string,
	mails map[string]repository.MailRecord,
) error {
	for _, table := range []string{attachmentExtraTable, attachmentsTable, metadataTable, mailsTable} {
		if _, err := database.ExecContext(ctx, "DELETE FROM "+table+" WHERE character_id = ?", characterID); err != nil {
			return err
		}
	}
	mailQuery := "INSERT INTO " + mailsTable + " (character_id, mail_id, sender_character_id, sender_name, recipient_character_id, recipient_name, title, body, gold, read_flag, claimed_flag, deleted_flag, created_at, expire_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	metadataQuery := "INSERT INTO " + metadataTable + " (character_id, mail_id, metadata_key, metadata_value) VALUES (?, ?, ?, ?)"
	attachmentQuery := "INSERT INTO " + attachmentsTable + " (character_id, mail_id, attachment_index, item_id, item_count, bind_flag, expire_at, raw_entry) VALUES (?, ?, ?, ?, ?, ?, ?, ?)"
	extraQuery := "INSERT INTO " + attachmentExtraTable + " (character_id, mail_id, attachment_index, extra_key, extra_value) VALUES (?, ?, ?, ?, ?)"
	for _, mailID := range sortedStringKeys(mails) {
		mail := mails[mailID]
		if _, err := database.ExecContext(
			ctx,
			mailQuery,
			characterID,
			mailID,
			mail.SenderCharacterID,
			mail.SenderName,
			mail.RecipientCharacterID,
			mail.RecipientName,
			mail.Title,
			mail.Body,
			mail.Gold,
			mail.Read,
			mail.Claimed,
			mail.Deleted,
			sqlTime(mail.CreatedAt),
			sqlTime(mail.ExpireAt),
		); err != nil {
			return err
		}
		for _, key := range sortedStringKeys(mail.Metadata) {
			if _, err := database.ExecContext(ctx, metadataQuery, characterID, mailID, key, mail.Metadata[key]); err != nil {
				return err
			}
		}
		for index, attachment := range mail.Attachments {
			if _, err := database.ExecContext(
				ctx,
				attachmentQuery,
				characterID,
				mailID,
				index,
				attachment.ItemID,
				attachment.Count,
				attachment.Bind,
				sqlTime(attachment.ExpireAt),
				attachment.RawEntry,
			); err != nil {
				return err
			}
			for _, key := range sortedStringKeys(attachment.Extra) {
				if _, err := database.ExecContext(ctx, extraQuery, characterID, mailID, index, key, attachment.Extra[key]); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
