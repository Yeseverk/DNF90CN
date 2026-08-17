#pragma once
#include <windows.h>
#include <unordered_map>

class InlineHook
{
public:
    static InlineHook& Instance()
    {
        static InlineHook inst;
        return inst;
    }

    bool Add(void* target, void* detour)
    {
        SIZE_T patchLen = CalcPatchLen(target, 5);
        if (patchLen < 5) return false;

        auto trampoline = (BYTE*)VirtualAlloc(NULL, patchLen + 5, MEM_COMMIT | MEM_RESERVE, PAGE_EXECUTE_READWRITE);
        if (!trampoline) return false;

        memcpy(trampoline, target, patchLen);
        trampoline[patchLen] = 0xE9;
        *(DWORD*)(trampoline + patchLen + 1) = (DWORD)((BYTE*)target + patchLen) - (DWORD)(trampoline + patchLen + 5);

        DWORD old;
        VirtualProtect(target, patchLen, PAGE_EXECUTE_READWRITE, &old);
        *(BYTE*)target = 0xE9;
        *(DWORD*)((BYTE*)target + 1) = (DWORD)(BYTE*)detour - (DWORD)((BYTE*)target + 5);
        for (SIZE_T i = 5; i < patchLen; i++)
            *((BYTE*)target + i) = 0x90;
        VirtualProtect(target, patchLen, old, &old);

        m_trampolines[(uintptr_t)target] = trampoline;
        return true;
    }

    void* GetTrampoline(uintptr_t target)
    {
        auto it = m_trampolines.find(target);
        return it != m_trampolines.end() ? it->second : nullptr;
    }

private:
    std::unordered_map<uintptr_t, BYTE*> m_trampolines;

    SIZE_T CalcPatchLen(void* addr, SIZE_T minLen)
    {
        SIZE_T len = 0;
        BYTE* p = (BYTE*)addr;
        while (len < minLen)
            len += InsnLen(p + len);
        return len;
    }

    SIZE_T InsnLen(BYTE* p)
    {
        if (p[0] == 0xCC || p[0] == 0xC3 || p[0] == 0x90) return 1;
        if (p[0] == 0x55 || p[0] == 0x53 || p[0] == 0x56 || p[0] == 0x57) return 1;
        if (p[0] == 0x5D || p[0] == 0x5B || p[0] == 0x5E || p[0] == 0x5F) return 1;
        if (p[0] == 0x50 || p[0] == 0x51 || p[0] == 0x52) return 1;
        if (p[0] == 0x58 || p[0] == 0x59 || p[0] == 0x5A) return 1;
        if (p[0] == 0x60 || p[0] == 0x61 || p[0] == 0x9C || p[0] == 0x9D) return 1;
        if (p[0] == 0x8B && (p[1] & 0xC0) == 0xC0) return 2;
        if (p[0] == 0x8B && p[1] == 0xEC) return 2;
        if (p[0] == 0x8B && p[1] == 0xFF) return 2;
        if (p[0] == 0x89 && (p[1] & 0xC0) == 0xC0) return 2;
        if (p[0] == 0x33 && (p[1] & 0xC0) == 0xC0) return 2;
        if (p[0] == 0x6A) return 2;
        if (p[0] == 0xEB) return 2;
        if (p[0] == 0xB8 || p[0] == 0xB9 || p[0] == 0xBA || p[0] == 0xBB ||
            p[0] == 0xBC || p[0] == 0xBD || p[0] == 0xBE || p[0] == 0xBF) return 5;
        if (p[0] == 0xE8 || p[0] == 0xE9) return 5;
        if (p[0] == 0x68) return 5;
        if (p[0] == 0x83 && p[1] == 0xEC) return 3;
        if (p[0] == 0x83 && p[1] == 0xE4) return 3;
        if (p[0] == 0x81 && p[1] == 0xEC) return 6;
        if (p[0] == 0xFF && p[1] == 0x25) return 6;
        if (p[0] == 0x8D && (p[1] & 0xC7) == 0x44) return 4;
        if (p[0] == 0x8D && (p[1] & 0xC0) == 0x40) return 3;
        if (p[0] == 0x64 && p[1] == 0xA1) return 6;
        return 1;
    }
};

inline BOOLEAN Hook_Inline(void* target, void* detour)
{
    return InlineHook::Instance().Add(target, detour) ? TRUE : FALSE;
}

inline void* Hook_GetTrampoline(uintptr_t target)
{
    return InlineHook::Instance().GetTrampoline(target);
}
