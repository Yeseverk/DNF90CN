// Local-debug compatibility for the current 32-bit DNF client.
//
// This implementation belongs only to the independent 90CN-debug test DLL.
// Production 90CN.dll neither compiles nor loads it. It does not hide debugger
// processes, patch game protocol functions, alter packets, or change
// NtSetContextThread. The current scope covers only the four native queries
// observed in the supplied 4026 investigation.

#include "debug_compat.h"

#include <windows.h>
#include <tlhelp32.h>
#include <intrin.h>
#include <stdint.h>
#include <stdio.h>
#include <stdarg.h>
#include <vector>

namespace
{
typedef LONG NTSTATUS_;
typedef NTSTATUS_ (NTAPI* NtQueryInformationProcessFn)(
    HANDLE, ULONG, PVOID, ULONG, PULONG);
typedef NTSTATUS_ (NTAPI* NtSetInformationThreadFn)(
    HANDLE, ULONG, PVOID, ULONG);
typedef NTSTATUS_ (NTAPI* NtQuerySystemInformationFn)(
    ULONG, PVOID, ULONG, PULONG);
typedef NTSTATUS_ (NTAPI* NtGetContextThreadFn)(HANDLE, PCONTEXT);

constexpr NTSTATUS_ kStatusSuccess = 0;
constexpr NTSTATUS_ kStatusPortNotSet = static_cast<NTSTATUS_>(0xC0000353);
constexpr ULONG kProcessDebugPort = 7;
constexpr ULONG kProcessDebugObjectHandle = 0x1E;
constexpr ULONG kProcessDebugFlags = 0x1F;
constexpr ULONG kThreadHideFromDebugger = 0x11;
constexpr ULONG kSystemKernelDebuggerInformation = 0x23;
constexpr DWORD kDebugRegisterFlag = 0x10;
constexpr DWORD kSupportedAbiVersion = 1;
constexpr size_t kPatchLength = 5;
constexpr size_t kTrampolineLength = 10;

struct NativeHook {
    const char* name;
    void* target;
    void* detour;
    void* trampoline;
    unsigned char original[kPatchLength];
    bool installed;
};

struct ProcessBasicInformation {
    NTSTATUS_ exitStatus;
    PVOID pebBaseAddress;
    ULONG_PTR affinityMask;
    LONG basePriority;
    ULONG_PTR uniqueProcessId;
    ULONG_PTR inheritedFromUniqueProcessId;
};

NativeHook g_hooks[] = {
    { "NtQueryInformationProcess", nullptr, nullptr, nullptr, {}, false },
    { "NtSetInformationThread", nullptr, nullptr, nullptr, {}, false },
    { "NtQuerySystemInformation", nullptr, nullptr, nullptr, {}, false },
    { "NtGetContextThread", nullptr, nullptr, nullptr, {}, false },
};

NtQueryInformationProcessFn g_originalNtQueryInformationProcess = nullptr;
NtSetInformationThreadFn g_originalNtSetInformationThread = nullptr;
NtQuerySystemInformationFn g_originalNtQuerySystemInformation = nullptr;
NtGetContextThreadFn g_originalNtGetContextThread = nullptr;

SRWLOCK g_logLock = SRWLOCK_INIT;
volatile LONG g_installState = 0;
volatile LONG g_scrubRunning = 0;
HANDLE g_stopEvent = nullptr;
HANDLE g_scrubThread = nullptr;
volatile LONG g_loggedDebugPort = 0;
volatile LONG g_loggedDebugObject = 0;
volatile LONG g_loggedDebugFlags = 0;
volatile LONG g_loggedHideThread = 0;
volatile LONG g_loggedKernelDebugger = 0;
volatile LONG g_loggedDebugRegisters = 0;

bool BuildGamePath(const wchar_t* fileName, wchar_t* output, size_t outputCount)
{
    if (!fileName || !output || outputCount == 0) return false;
    output[0] = L'\0';
    if (!GetModuleFileNameW(nullptr, output, static_cast<DWORD>(outputCount))) {
        return false;
    }
    wchar_t* slash = wcsrchr(output, L'\\');
    if (!slash) return false;
    slash[1] = L'\0';
    return wcsncat_s(output, outputCount, fileName, _TRUNCATE) == 0;
}

void DebugLog(const char* format, ...)
{
    wchar_t logPath[MAX_PATH] = { 0 };
    if (!BuildGamePath(L"90CN-debug.log", logPath, _countof(logPath))) return;

    char line[1024] = { 0 };
    SYSTEMTIME now = {};
    GetLocalTime(&now);
    int offset = _snprintf(line, sizeof(line) - 2,
        "[%04u-%02u-%02u %02u:%02u:%02u.%03u] [DEBUG-COMPAT] [pid=%u tid=%u] ",
        now.wYear, now.wMonth, now.wDay, now.wHour, now.wMinute, now.wSecond,
        now.wMilliseconds, GetCurrentProcessId(), GetCurrentThreadId());
    if (offset < 0) offset = 0;

    va_list args;
    va_start(args, format);
    int wrote = _vsnprintf(line + offset, sizeof(line) - 2 - offset, format, args);
    va_end(args);
    int length = wrote >= 0 ? offset + wrote : static_cast<int>(sizeof(line) - 2);
    line[length++] = '\n';

    AcquireSRWLockExclusive(&g_logLock);
    HANDLE file = CreateFileW(logPath, FILE_APPEND_DATA,
        FILE_SHARE_READ | FILE_SHARE_WRITE, nullptr, OPEN_ALWAYS,
        FILE_ATTRIBUTE_NORMAL, nullptr);
    if (file != INVALID_HANDLE_VALUE) {
        DWORD ignored = 0;
        WriteFile(file, line, static_cast<DWORD>(length), &ignored, nullptr);
        CloseHandle(file);
    }
    ReleaseSRWLockExclusive(&g_logLock);
}

void LogOnce(volatile LONG* flag, const char* text)
{
    if (InterlockedCompareExchange(flag, 1, 0) == 0) {
        DebugLog("%s", text);
    }
}

bool NtSucceeded(NTSTATUS_ status)
{
    return status >= 0;
}

bool IsCurrentProcessHandle(HANDLE process)
{
    if (process == GetCurrentProcess() ||
        process == reinterpret_cast<HANDLE>(static_cast<LONG_PTR>(-1))) {
        return true;
    }
    if (!process || !g_originalNtQueryInformationProcess) return false;

    ProcessBasicInformation basic = {};
    ULONG returned = 0;
    NTSTATUS_ status = g_originalNtQueryInformationProcess(
        process, 0, &basic, sizeof(basic), &returned);
    return NtSucceeded(status) &&
        basic.uniqueProcessId == static_cast<ULONG_PTR>(GetCurrentProcessId());
}

NTSTATUS_ NTAPI HookNtQueryInformationProcess(
    HANDLE process, ULONG informationClass, PVOID information,
    ULONG informationLength, PULONG returnLength)
{
    NTSTATUS_ status = g_originalNtQueryInformationProcess(
        process, informationClass, information, informationLength, returnLength);
    if (!IsCurrentProcessHandle(process) || !information) return status;

    if (informationClass == kProcessDebugPort &&
        informationLength >= sizeof(ULONG_PTR)) {
        *reinterpret_cast<ULONG_PTR*>(information) = 0;
        LogOnce(&g_loggedDebugPort,
            "sanitized NtQueryInformationProcess(ProcessDebugPort)");
        return NtSucceeded(status) ? status : kStatusSuccess;
    }
    if (informationClass == kProcessDebugObjectHandle &&
        informationLength >= sizeof(ULONG_PTR)) {
        *reinterpret_cast<ULONG_PTR*>(information) = 0;
        LogOnce(&g_loggedDebugObject,
            "sanitized NtQueryInformationProcess(ProcessDebugObjectHandle)");
        return kStatusPortNotSet;
    }
    if (informationClass == kProcessDebugFlags &&
        informationLength >= sizeof(ULONG)) {
        *reinterpret_cast<ULONG*>(information) = 1;
        LogOnce(&g_loggedDebugFlags,
            "sanitized NtQueryInformationProcess(ProcessDebugFlags)");
        return kStatusSuccess;
    }
    return status;
}

NTSTATUS_ NTAPI HookNtSetInformationThread(
    HANDLE thread, ULONG informationClass, PVOID information,
    ULONG informationLength)
{
    if (informationClass == kThreadHideFromDebugger) {
        LogOnce(&g_loggedHideThread,
            "ignored NtSetInformationThread(ThreadHideFromDebugger)");
        return kStatusSuccess;
    }
    return g_originalNtSetInformationThread(
        thread, informationClass, information, informationLength);
}

NTSTATUS_ NTAPI HookNtQuerySystemInformation(
    ULONG informationClass, PVOID information, ULONG informationLength,
    PULONG returnLength)
{
    NTSTATUS_ status = g_originalNtQuerySystemInformation(
        informationClass, information, informationLength, returnLength);
    if (informationClass == kSystemKernelDebuggerInformation &&
        NtSucceeded(status) && information && informationLength >= 2) {
        unsigned char* bytes = static_cast<unsigned char*>(information);
        bytes[0] = 0;
        bytes[1] = 1;
        LogOnce(&g_loggedKernelDebugger,
            "sanitized NtQuerySystemInformation(SystemKernelDebuggerInformation)");
    }
    return status;
}

NTSTATUS_ NTAPI HookNtGetContextThread(HANDLE thread, PCONTEXT context)
{
    NTSTATUS_ status = g_originalNtGetContextThread(thread, context);
    if (NtSucceeded(status) && context &&
        (context->ContextFlags & kDebugRegisterFlag) != 0) {
        context->Dr0 = 0;
        context->Dr1 = 0;
        context->Dr2 = 0;
        context->Dr3 = 0;
        context->Dr6 = 0;
        context->Dr7 = 0;
        LogOnce(&g_loggedDebugRegisters,
            "sanitized debug-register values returned by NtGetContextThread");
    }
    return status;
}

void ScrubPebDebugScalars()
{
    __try {
        unsigned char* peb = reinterpret_cast<unsigned char*>(__readfsdword(0x30));
        if (!peb) return;
        peb[2] = 0;
        DWORD* globalFlags = reinterpret_cast<DWORD*>(peb + 0x68);
        *globalFlags &= ~static_cast<DWORD>(0x70);
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
    }
}

DWORD WINAPI ScrubWorker(void*)
{
    while (InterlockedCompareExchange(&g_scrubRunning, 1, 1) != 0) {
        ScrubPebDebugScalars();
        HANDLE eventHandle = g_stopEvent;
        if (!eventHandle || WaitForSingleObject(eventHandle, 10) == WAIT_OBJECT_0) {
            break;
        }
    }
    return 0;
}

void ReleasePreparedHooks()
{
    for (NativeHook& hook : g_hooks) {
        if (hook.trampoline) {
            VirtualFree(hook.trampoline, 0, MEM_RELEASE);
            hook.trampoline = nullptr;
        }
    }
}

bool PrepareHook(NativeHook* hook, HMODULE ntdll, const char* name, void* detour)
{
    hook->name = name;
    hook->target = reinterpret_cast<void*>(GetProcAddress(ntdll, name));
    hook->detour = detour;
    hook->installed = false;
    if (!hook->target) {
        DebugLog("resolve failed name=%s error=%u", name, GetLastError());
        return false;
    }

    __try {
        memcpy(hook->original, hook->target, kPatchLength);
    }
    __except (EXCEPTION_EXECUTE_HANDLER) {
        DebugLog("read target failed name=%s code=0x%08X", name, GetExceptionCode());
        return false;
    }

    // Current 32-bit Windows ntdll syscall entries begin with MOV EAX, imm32.
    // Refuse to patch an unknown or already-hooked layout.
    if (hook->original[0] != 0xB8) {
        DebugLog("unsupported or pre-hooked ntdll entry name=%s bytes=%02X %02X %02X %02X %02X",
            name, hook->original[0], hook->original[1], hook->original[2],
            hook->original[3], hook->original[4]);
        return false;
    }

    unsigned char* trampoline = static_cast<unsigned char*>(VirtualAlloc(
        nullptr, kTrampolineLength, MEM_COMMIT | MEM_RESERVE,
        PAGE_EXECUTE_READWRITE));
    if (!trampoline) {
        DebugLog("trampoline allocation failed name=%s error=%u", name, GetLastError());
        return false;
    }
    memcpy(trampoline, hook->original, kPatchLength);
    trampoline[kPatchLength] = 0xE9;
    intptr_t resumeDelta =
        (reinterpret_cast<unsigned char*>(hook->target) + kPatchLength) -
        (trampoline + kTrampolineLength);
    *reinterpret_cast<int32_t*>(trampoline + kPatchLength + 1) =
        static_cast<int32_t>(resumeDelta);
    FlushInstructionCache(GetCurrentProcess(), trampoline, kTrampolineLength);
    hook->trampoline = trampoline;
    return true;
}

std::vector<HANDLE> SuspendOtherThreads()
{
    std::vector<HANDLE> suspended;
    HANDLE snapshot = CreateToolhelp32Snapshot(TH32CS_SNAPTHREAD, 0);
    if (snapshot == INVALID_HANDLE_VALUE) return suspended;

    THREADENTRY32 entry = {};
    entry.dwSize = sizeof(entry);
    DWORD currentProcess = GetCurrentProcessId();
    DWORD currentThread = GetCurrentThreadId();
    if (Thread32First(snapshot, &entry)) {
        do {
            if (entry.th32OwnerProcessID != currentProcess ||
                entry.th32ThreadID == currentThread) {
                continue;
            }
            HANDLE thread = OpenThread(
                THREAD_SUSPEND_RESUME | THREAD_QUERY_INFORMATION,
                FALSE, entry.th32ThreadID);
            if (!thread) continue;
            if (SuspendThread(thread) == static_cast<DWORD>(-1)) {
                CloseHandle(thread);
                continue;
            }
            suspended.push_back(thread);
        } while (Thread32Next(snapshot, &entry));
    }
    CloseHandle(snapshot);
    return suspended;
}

void ResumeThreads(std::vector<HANDLE>* threads)
{
    if (!threads) return;
    for (HANDLE thread : *threads) {
        ResumeThread(thread);
        CloseHandle(thread);
    }
    threads->clear();
}

bool WriteHook(NativeHook* hook, bool install)
{
    unsigned char patch[kPatchLength] = {};
    const unsigned char* source = hook->original;
    if (install) {
        patch[0] = 0xE9;
        intptr_t delta = reinterpret_cast<unsigned char*>(hook->detour) -
            (reinterpret_cast<unsigned char*>(hook->target) + kPatchLength);
        *reinterpret_cast<int32_t*>(patch + 1) = static_cast<int32_t>(delta);
        source = patch;
    }

    DWORD oldProtection = 0;
    if (!VirtualProtect(hook->target, kPatchLength,
            PAGE_EXECUTE_READWRITE, &oldProtection)) {
        DebugLog("VirtualProtect failed name=%s error=%u",
            hook->name, GetLastError());
        return false;
    }
    memcpy(hook->target, source, kPatchLength);
    FlushInstructionCache(GetCurrentProcess(), hook->target, kPatchLength);
    DWORD ignoredProtection = 0;
    VirtualProtect(hook->target, kPatchLength, oldProtection, &ignoredProtection);
    hook->installed = install;
    return true;
}

bool InstallNativeHooks()
{
    HMODULE ntdll = GetModuleHandleW(L"ntdll.dll");
    if (!ntdll) {
        DebugLog("ntdll.dll is not loaded");
        return false;
    }

    if (!PrepareHook(&g_hooks[0], ntdll, "NtQueryInformationProcess",
            reinterpret_cast<void*>(&HookNtQueryInformationProcess)) ||
        !PrepareHook(&g_hooks[1], ntdll, "NtSetInformationThread",
            reinterpret_cast<void*>(&HookNtSetInformationThread)) ||
        !PrepareHook(&g_hooks[2], ntdll, "NtQuerySystemInformation",
            reinterpret_cast<void*>(&HookNtQuerySystemInformation)) ||
        !PrepareHook(&g_hooks[3], ntdll, "NtGetContextThread",
            reinterpret_cast<void*>(&HookNtGetContextThread))) {
        ReleasePreparedHooks();
        return false;
    }

    g_originalNtQueryInformationProcess =
        reinterpret_cast<NtQueryInformationProcessFn>(g_hooks[0].trampoline);
    g_originalNtSetInformationThread =
        reinterpret_cast<NtSetInformationThreadFn>(g_hooks[1].trampoline);
    g_originalNtQuerySystemInformation =
        reinterpret_cast<NtQuerySystemInformationFn>(g_hooks[2].trampoline);
    g_originalNtGetContextThread =
        reinterpret_cast<NtGetContextThreadFn>(g_hooks[3].trampoline);

    std::vector<HANDLE> suspended = SuspendOtherThreads();
    bool ok = true;
    size_t installedCount = 0;
    for (NativeHook& hook : g_hooks) {
        if (!WriteHook(&hook, true)) {
            ok = false;
            break;
        }
        ++installedCount;
    }
    if (!ok) {
        while (installedCount > 0) {
            --installedCount;
            WriteHook(&g_hooks[installedCount], false);
        }
    }
    ResumeThreads(&suspended);
    if (!ok) {
        ReleasePreparedHooks();
        return false;
    }

    for (const NativeHook& hook : g_hooks) {
        DebugLog("hook installed name=%s target=%p trampoline=%p",
            hook.name, hook.target, hook.trampoline);
    }
    return true;
}

bool StartPebScrubber()
{
    ScrubPebDebugScalars();
    g_stopEvent = CreateEventW(nullptr, TRUE, FALSE, nullptr);
    if (!g_stopEvent) {
        DebugLog("CreateEventW failed error=%u", GetLastError());
        return false;
    }
    InterlockedExchange(&g_scrubRunning, 1);
    g_scrubThread = CreateThread(nullptr, 0, ScrubWorker, nullptr, 0, nullptr);
    if (!g_scrubThread) {
        DebugLog("PEB scrub thread creation failed error=%u", GetLastError());
        InterlockedExchange(&g_scrubRunning, 0);
        CloseHandle(g_stopEvent);
        g_stopEvent = nullptr;
        return false;
    }
    return true;
}
} // namespace

extern "C" BOOL WINAPI Install90CNDebugCompat(
    unsigned int abiVersion)
{
    LONG prior = InterlockedCompareExchange(&g_installState, 1, 0);
    if (prior == 2) return TRUE;
    if (prior != 0) return FALSE;

    DebugLog("installer entered abi=%u pointer_size=%u",
        abiVersion, static_cast<unsigned int>(sizeof(void*)));
    if (abiVersion != kSupportedAbiVersion || sizeof(void*) != 4) {
        DebugLog("installer rejected abi=%u pointer_size=%u",
            abiVersion, static_cast<unsigned int>(sizeof(void*)));
        InterlockedExchange(&g_installState, -1);
        return FALSE;
    }
    if (!InstallNativeHooks()) {
        DebugLog("installer failed while installing native hooks");
        InterlockedExchange(&g_installState, -1);
        return FALSE;
    }
    bool scrubberStarted = StartPebScrubber();
    InterlockedExchange(&g_installState, 2);
    DebugLog("debug compatibility installed scrubber=%d; attach debugger only after this line",
        scrubberStarted ? 1 : 0);
    return TRUE;
}
