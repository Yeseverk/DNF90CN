package channelinfo

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Build90CNOnlineScript derives the channel table downloaded by the 90CN
// client from the raw PVF channel_info.etc. Source names and groups remain
// authoritative; only the advertised server ID and proved online type
// projection may differ from the raw in-game catalog.
func Build90CNOnlineScript(data []byte, sourceServerID, advertiseServerID, bootstrapChannelID int) ([]byte, error) {
	if sourceServerID <= 0 || advertiseServerID < 0 || bootstrapChannelID <= 0 {
		return nil, errors.New("channel script source, advertisement, and bootstrap IDs are invalid")
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	var prefix strings.Builder
	serverSectionSeen := false
	inServer := false
	activeServer := 1
	records := make([][]string, 0, 64)
	bootstrapFound := false

	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if serverID, ok, err := parseServerSectionHeader(trimmed); ok {
			if err != nil {
				return nil, fmt.Errorf("channel script line %d: %w", lineNumber, err)
			}
			serverSectionSeen = true
			inServer = true
			activeServer = serverID
			continue
		}
		switch strings.ToLower(trimmed) {
		case "[/server]":
			inServer = false
			continue
		}
		if !serverSectionSeen {
			prefix.WriteString(line)
			prefix.WriteByte('\n')
			continue
		}
		if !inServer {
			continue
		}

		if trimmed == "" {
			continue
		}
		fields := splitFields(trimmed)
		if len(fields) == 0 {
			continue
		}
		if len(fields) == 1 {
			if id, err := strconv.Atoi(fields[0]); err == nil {
				activeServer = id
			}
			continue
		}

		start := 0
		if len(fields) >= 5 && isInt(fields[0]) && isChannelStart(fields, 1) {
			activeServer, _ = strconv.Atoi(fields[0])
			start = 1
		}
		for start < len(fields) {
			if !isChannelStart(fields, start) {
				return nil, fmt.Errorf("channel script line %d: invalid packed channel row", lineNumber)
			}
			end := len(fields)
			for next := start + 4; next < len(fields); next++ {
				if isChannelStart(fields, next) {
					end = next
					break
				}
			}
			if activeServer == sourceServerID {
				record := append([]string(nil), fields[start:end]...)
				id, _ := strconv.Atoi(record[0])
				channelType, _ := strconv.Atoi(record[2])
				record[2] = strconv.Itoa(OnlineTypeFor90CN(id, channelType))
				if id == bootstrapChannelID {
					bootstrapFound = true
				}
				for len(record) > 0 && record[len(record)-1] == "``" {
					record = record[:len(record)-1]
				}
				record = append(record, "``")
				records = append(records, record)
			}
			start = end
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("channel script source server %d has no channels", sourceServerID)
	}
	if !bootstrapFound {
		return nil, fmt.Errorf("channel script source server %d is missing bootstrap channel %d", sourceServerID, bootstrapChannelID)
	}

	var output strings.Builder
	output.WriteString(prefix.String())
	output.WriteString("[server]\n")
	output.WriteString(strconv.Itoa(advertiseServerID))
	output.WriteByte('\n')
	for _, record := range records {
		output.WriteString(strings.Join(record, " "))
		output.WriteByte('\n')
	}
	output.WriteString("[/server]\n")
	return []byte(output.String()), nil
}

// OnlineTypeFor90CN converts a raw PVF channel type into the type stored in
// the downloaded online directory. It must never be used for game CHANNELINFO.
func OnlineTypeFor90CN(channelID, channelType int) int {
	if channelType == 0 || channelType == 1 {
		return 22
	}
	if channelID == 1 && channelType == 2 {
		return 11
	}
	return channelType
}
