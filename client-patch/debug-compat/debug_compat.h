#pragma once

#include <windows.h>

// Shared entry used by the production 90CN.dll and the isolated smoke-test
// harness. The implementation is compiled directly into 90CN.dll; no runtime
// companion DLL is required.
extern "C" __declspec(dllexport) BOOL WINAPI Install90CNDebugCompat(
    unsigned int abiVersion);
