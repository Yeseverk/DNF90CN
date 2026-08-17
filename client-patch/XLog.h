#include <windows.h>
#include <iostream>
#include <fstream>
#include <iomanip>
#include <vector>
#include <string>
#include <sstream>
#include <fcntl.h>
#include <io.h>


static std::wstring HexToWString(const unsigned char* data, size_t length)
{
	std::wstringstream wss;
	wss << std::hex << std::uppercase << std::setfill(L'0');
	for (size_t i = 0; i < length; ++i) {
		wss << std::setw(2) << static_cast<int>(data[i]);
		if (i != length - 1) {
			wss << L" ";
		}
	}
	return wss.str();
}

static std::vector<std::wstring> HexToWStrings(const unsigned char* data, size_t length, size_t bytesPerLine = 16)
{
	std::vector<std::wstring> lines;
	std::wstringstream wss;
	wss << std::hex << std::uppercase << std::setfill(L'0');
	for (size_t i = 0; i < length; ++i) {
		wss << std::setw(2) << static_cast<int>(data[i]);
		if ((i + 1) % bytesPerLine == 0 || i == length - 1) {
			lines.push_back(wss.str());
			wss.str(L"");
			wss.clear();
		}
		else {
			wss << L" ";
		}
	}
	return lines;
}


static void AppendFileLogLine(const wchar_t* filename, const std::wstring& line)
{
	FILE* fp = NULL;
	_wfopen_s(&fp, filename, L"a, ccs=UTF-8");
	if (fp) {
		fwprintf(fp, L"%s\n", line.c_str());
		fclose(fp);
	}
}

static void AppendFileLogLines(const wchar_t* filename, const std::vector<std::wstring>& lines)
{
	FILE* fp = NULL;
	_wfopen_s(&fp, filename, L"a, ccs=UTF-8");
	if (fp) {
		for (const auto& line : lines) {
			fwprintf(fp, L"%s\n", line.c_str());
		}
		fclose(fp);
	}
}

static void AppendFileLogFormatLine(const wchar_t* filename, const wchar_t* format, ...)
{
	va_list args;
	va_start(args, format);
	int neededSize = _vscwprintf(format, args) + 1;
	va_end(args);

	wchar_t stackBuffer[2048];
	wchar_t* buffer = stackBuffer;
	if (neededSize > _countof(stackBuffer)) {
		buffer = (wchar_t*)malloc(neededSize * sizeof(wchar_t));
		if (!buffer) return;
	}

	va_start(args, format);
	vswprintf_s(buffer, neededSize > _countof(stackBuffer) ? neededSize : _countof(stackBuffer), format, args);
	va_end(args);

	AppendFileLogLine(filename, std::wstring(buffer));

	if (buffer != stackBuffer)
		free(buffer);
}

static void AppendHexLogLine(const wchar_t* filename, const unsigned char* data, size_t length)
{
	std::wstring hexString = HexToWString(data, length);
	AppendFileLogLine(filename, hexString);
}

static void AppendHexLogLines(const wchar_t* filename, const unsigned char* data, size_t length, size_t bytesPerLine = 16)
{
	std::vector<std::wstring> hexLines = HexToWStrings(data, length, bytesPerLine);
	AppendFileLogLines(filename, hexLines);
}
