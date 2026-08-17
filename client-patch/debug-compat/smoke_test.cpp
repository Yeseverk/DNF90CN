#include <windows.h>
#include <intrin.h>
#include <stdint.h>
#include <stdio.h>

namespace
{
typedef LONG NTSTATUS_;
typedef NTSTATUS_ (NTAPI* NtQueryInformationProcessFn)(
    HANDLE, ULONG, PVOID, ULONG, PULONG);
typedef NTSTATUS_ (NTAPI* NtQuerySystemInformationFn)(
    ULONG, PVOID, ULONG, PULONG);
typedef NTSTATUS_ (NTAPI* NtSetInformationThreadFn)(
    HANDLE, ULONG, PVOID, ULONG);
typedef NTSTATUS_ (NTAPI* NtGetContextThreadFn)(HANDLE, PCONTEXT);
typedef BOOL (WINAPI* InstallDebugCompatibilityFn)(unsigned int);

DWORD WINAPI IdleThread(void*)
{
    return 0;
}

bool Expect(bool condition, const char* label)
{
    printf("%s: %s\n", label, condition ? "PASS" : "FAIL");
    return condition;
}
} // namespace

int wmain(int argc, wchar_t** argv)
{
    const wchar_t* dllPath = argc > 1 ? argv[1] : L"90CN-debug.dll";
    HMODULE debugModule = LoadLibraryW(dllPath);
    if (!Expect(debugModule != nullptr, "LoadLibraryW")) return 1;

    InstallDebugCompatibilityFn install =
        reinterpret_cast<InstallDebugCompatibilityFn>(
            GetProcAddress(debugModule, "Install90CNDebugCompat"));
    if (!Expect(install != nullptr, "GetProcAddress")) return 1;
    if (!Expect(install(1) == TRUE, "Install90CNDebugCompat")) return 1;

    HMODULE ntdll = GetModuleHandleW(L"ntdll.dll");
    NtQueryInformationProcessFn queryProcess =
        reinterpret_cast<NtQueryInformationProcessFn>(
            GetProcAddress(ntdll, "NtQueryInformationProcess"));
    NtQuerySystemInformationFn querySystem =
        reinterpret_cast<NtQuerySystemInformationFn>(
            GetProcAddress(ntdll, "NtQuerySystemInformation"));
    NtSetInformationThreadFn setThreadInformation =
        reinterpret_cast<NtSetInformationThreadFn>(
            GetProcAddress(ntdll, "NtSetInformationThread"));
    NtGetContextThreadFn getContext =
        reinterpret_cast<NtGetContextThreadFn>(
            GetProcAddress(ntdll, "NtGetContextThread"));
    bool ok = Expect(queryProcess && querySystem && setThreadInformation &&
        getContext, "ResolveNativeQueries");

    ULONG_PTR debugPort = static_cast<ULONG_PTR>(-1);
    NTSTATUS_ status = queryProcess(
        GetCurrentProcess(), 7, &debugPort, sizeof(debugPort), nullptr);
    ok &= Expect(status >= 0 && debugPort == 0, "ProcessDebugPort");

    ULONG_PTR debugObject = static_cast<ULONG_PTR>(-1);
    status = queryProcess(
        GetCurrentProcess(), 0x1E, &debugObject, sizeof(debugObject), nullptr);
    ok &= Expect(status == static_cast<NTSTATUS_>(0xC0000353) &&
        debugObject == 0, "ProcessDebugObjectHandle");

    ULONG debugFlags = 0;
    status = queryProcess(
        GetCurrentProcess(), 0x1F, &debugFlags, sizeof(debugFlags), nullptr);
    ok &= Expect(status >= 0 && debugFlags == 1, "ProcessDebugFlags");

    HANDLE realProcessHandle = nullptr;
    ok &= Expect(DuplicateHandle(
        GetCurrentProcess(), GetCurrentProcess(), GetCurrentProcess(),
        &realProcessHandle, PROCESS_QUERY_INFORMATION, FALSE, 0) == TRUE,
        "DuplicateRealProcessHandle");
    if (realProcessHandle) {
        debugPort = static_cast<ULONG_PTR>(-1);
        status = queryProcess(
            realProcessHandle, 7, &debugPort, sizeof(debugPort), nullptr);
        ok &= Expect(status >= 0 && debugPort == 0,
            "RealHandleProcessDebugPort");
        CloseHandle(realProcessHandle);
    }

    status = setThreadInformation(
        reinterpret_cast<HANDLE>(static_cast<LONG_PTR>(-2)),
        0x11, nullptr, 0);
    ok &= Expect(status >= 0, "ThreadHideFromDebuggerIgnored");

    unsigned char kernelDebugger[2] = { 1, 0 };
    status = querySystem(0x23, kernelDebugger, sizeof(kernelDebugger), nullptr);
    ok &= Expect(status >= 0 && kernelDebugger[0] == 0 &&
        kernelDebugger[1] == 1, "SystemKernelDebuggerInformation");

    unsigned char* peb =
        reinterpret_cast<unsigned char*>(__readfsdword(0x30));
    peb[2] = 1;
    *reinterpret_cast<DWORD*>(peb + 0x68) |= 0x70;
    Sleep(40);
    ok &= Expect(peb[2] == 0 &&
        (*reinterpret_cast<DWORD*>(peb + 0x68) & 0x70) == 0,
        "PebScrubber");

    DWORD threadID = 0;
    HANDLE thread = CreateThread(
        nullptr, 0, IdleThread, nullptr, CREATE_SUSPENDED, &threadID);
    ok &= Expect(thread != nullptr, "CreateSuspendedThread");
    if (thread) {
        CONTEXT setContext = {};
        setContext.ContextFlags = CONTEXT_DEBUG_REGISTERS;
        setContext.Dr0 = 0x12345678;
        setContext.Dr7 = 1;
        ok &= Expect(SetThreadContext(thread, &setContext) == TRUE,
            "SetHardwareBreakpointState");

        CONTEXT readContext = {};
        readContext.ContextFlags = CONTEXT_DEBUG_REGISTERS;
        status = getContext(thread, &readContext);
        ok &= Expect(status >= 0 && readContext.Dr0 == 0 &&
            readContext.Dr1 == 0 && readContext.Dr2 == 0 &&
            readContext.Dr3 == 0 && readContext.Dr6 == 0 &&
            readContext.Dr7 == 0, "SanitizedHardwareBreakpointQuery");
        ResumeThread(thread);
        WaitForSingleObject(thread, 1000);
        CloseHandle(thread);
    }

    printf("RESULT: %s\n", ok ? "PASS" : "FAIL");
    return ok ? 0 : 1;
}
