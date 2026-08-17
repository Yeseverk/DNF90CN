//go:build linux && amd64 && cgo

package nativepatch

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <unistd.h>

typedef struct {
	void *addr;
	unsigned char original[12];
} lh_patch_slot;

static lh_patch_slot lh_patches[100];
static int lh_patch_count = 0;

static int lh_mprotect_range(void *addr, size_t len, int prot) {
	size_t page_size = (size_t)sysconf(_SC_PAGESIZE);
	uintptr_t start = (uintptr_t)addr & ~(uintptr_t)(page_size - 1);
	uintptr_t end = ((uintptr_t)addr + len + page_size - 1) & ~(uintptr_t)(page_size - 1);
	return mprotect((void*)start, end - start, prot);
}

static int lh_patch_symbol(const char *old_name, void *new_func) {
	if (new_func == NULL) {
		return -1;
	}
	if (lh_patch_count >= 100) {
		return -99;
	}
	void *old_func = NULL;
	if (old_name != NULL && strlen(old_name) > 2 && old_name[0] == '0' && old_name[1] == 'x') {
		old_func = (void*)strtoull(old_name, NULL, 16);
	} else {
		old_func = dlsym(NULL, old_name);
	}
	if (old_func == NULL) {
		return -2;
	}
	if (old_func == new_func) {
		return -3;
	}
	unsigned char jump[12] = {0x48, 0xba, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xe2};
	memcpy(&jump[2], &new_func, sizeof(void*));
	if (lh_mprotect_range(old_func, sizeof(jump), PROT_READ|PROT_WRITE|PROT_EXEC) != 0) {
		return -4;
	}
	lh_patch_slot *slot = &lh_patches[lh_patch_count];
	memcpy(slot->original, old_func, sizeof(jump));
	slot->addr = old_func;
	memcpy(old_func, jump, sizeof(jump));
	__builtin___clear_cache((char*)old_func, (char*)old_func + sizeof(jump));
	lh_patch_count++;
	if (lh_mprotect_range(old_func, sizeof(jump), PROT_READ|PROT_EXEC) != 0) {
		return -5;
	}
	return 0;
}

static int lh_restore_symbols() {
	int i;
	int kept = 0;
	for (i = lh_patch_count - 1; i >= 0; i--) {
		lh_patch_slot *slot = &lh_patches[i];
		if (lh_mprotect_range(slot->addr, sizeof(slot->original), PROT_READ|PROT_WRITE|PROT_EXEC) != 0) {
			lh_patches[kept++] = *slot;
			continue;
		}
		memcpy(slot->addr, slot->original, sizeof(slot->original));
		__builtin___clear_cache((char*)slot->addr, (char*)slot->addr + sizeof(slot->original));
		(void)lh_mprotect_range(slot->addr, sizeof(slot->original), PROT_READ|PROT_EXEC);
	}
	lh_patch_count = kept;
	return kept;
}

static int lh_patch_symbol_addr(const char *old_name, uintptr_t new_func_addr) {
	return lh_patch_symbol(old_name, (void*)new_func_addr);
}
*/
import "C"

import (
	"context"
	"fmt"
	"path/filepath"
	"plugin"
	"reflect"
	"sync"
	"time"
	"unsafe"
)

type NativeEngine struct {
	mu       sync.Mutex
	snapshot Snapshot
	plugins  []*plugin.Plugin
}

func NewNativeEngine() *NativeEngine {
	return defaultNativeEngine
}

var defaultNativeEngine = &NativeEngine{snapshot: Snapshot{Supported: true}}

func (e *NativeEngine) Supported() bool {
	return true
}

func (e *NativeEngine) Apply(ctx context.Context, pkg Package) (Result, error) {
	if err := contextErr(ctx); err != nil {
		return Result{}, err
	}
	if err := ValidatePlan(pkg.Plan); err != nil {
		return Result{}, err
	}
	started := time.Now().UTC()
	e.mu.Lock()
	defer e.mu.Unlock()
	// 应用新补丁前先恢复旧补丁，保证进程内同一时刻只有一组函数入口被改写。
	if remaining := int(C.lh_restore_symbols()); remaining != 0 {
		return Result{Version: pkg.Plan.Version, Target: pkg.Plan.Target, StartedAt: started, EndedAt: time.Now().UTC()}, fmt.Errorf("restore existing native patch left %d symbol(s)", remaining)
	}
	e.plugins = nil
	applied, plugins, err := applyPackage(ctx, pkg)
	if err != nil {
		// 任意符号替换失败都尝试回滚已替换部分，避免半套补丁继续运行。
		if remaining := int(C.lh_restore_symbols()); remaining != 0 {
			err = fmt.Errorf("%w; cleanup left %d native patch symbol(s)", err, remaining)
		}
		e.snapshot = Snapshot{Supported: true}
		return Result{Version: pkg.Plan.Version, Target: pkg.Plan.Target, StartedAt: started, EndedAt: time.Now().UTC()}, err
	}
	e.plugins = plugins
	ended := time.Now().UTC()
	e.snapshot = Snapshot{
		Supported:      true,
		Applied:        true,
		Version:        pkg.Plan.Version,
		Target:         pkg.Plan.Target,
		AppliedSymbols: applied,
		LastAppliedAt:  ended,
	}
	return Result{Version: pkg.Plan.Version, Target: pkg.Plan.Target, Applied: applied, StartedAt: started, EndedAt: ended}, nil
}

func (e *NativeEngine) Restore(ctx context.Context) (Result, error) {
	if err := contextErr(ctx); err != nil {
		return Result{}, err
	}
	started := time.Now().UTC()
	e.mu.Lock()
	defer e.mu.Unlock()
	// Restore 必须在同一把锁内完成，避免 Apply/Restore 并发改写同一段函数入口。
	if remaining := int(C.lh_restore_symbols()); remaining != 0 {
		ended := time.Now().UTC()
		e.snapshot.Applied = true
		return Result{Restored: false, StartedAt: started, EndedAt: ended}, fmt.Errorf("restore native patch left %d symbol(s)", remaining)
	}
	e.plugins = nil
	ended := time.Now().UTC()
	e.snapshot = Snapshot{Supported: true, LastRestoredAt: ended}
	return Result{Restored: true, StartedAt: started, EndedAt: ended}, nil
}

func (e *NativeEngine) Snapshot() Snapshot {
	e.mu.Lock()
	snapshot := e.snapshot
	e.mu.Unlock()
	return snapshot
}

func applyPackage(ctx context.Context, pkg Package) (int, []*plugin.Plugin, error) {
	applied := 0
	loaded := make([]*plugin.Plugin, 0, len(pkg.Plan.Plugins))
	for _, item := range pkg.Plan.Plugins {
		if err := contextErr(ctx); err != nil {
			return applied, loaded, err
		}
		plug, err := plugin.Open(resolvePluginPath(pkg.Directory, item.File))
		if err != nil {
			return applied, loaded, fmt.Errorf("open native patch plugin %s: %w", item.File, err)
		}
		loaded = append(loaded, plug)
		// symbols 映射含义是“新函数符号 -> 旧函数符号”，旧函数入口会被跳转到新函数。
		// 签名兼容必须由发布流程和人工评审保证，运行时只能校验符号存在。
		for newSymbol, oldSymbol := range item.Symbols {
			symbol, err := plug.Lookup(newSymbol)
			if err != nil {
				return applied, loaded, fmt.Errorf("lookup native patch symbol %s: %w", newSymbol, err)
			}
			ptr, err := functionPointer(reflect.ValueOf(symbol))
			if err != nil {
				return applied, loaded, fmt.Errorf("native patch symbol %s: %w", newSymbol, err)
			}
			oldName := C.CString(oldSymbol)
			code := C.lh_patch_symbol_addr(oldName, ptr)
			C.free(unsafe.Pointer(oldName))
			if code != 0 {
				return applied, loaded, fmt.Errorf("patch native symbol %s -> %s failed: code=%d", newSymbol, oldSymbol, int(code))
			}
			applied++
		}
	}
	return applied, loaded, nil
}

func resolvePluginPath(root, file string) string {
	return filepath.Join(root, file)
}

func functionPointer(value reflect.Value) (C.uintptr_t, error) {
	if !value.IsValid() || value.Kind() != reflect.Func {
		return 0, fmt.Errorf("exported symbol must be a function")
	}
	ptr := value.Pointer()
	if ptr == 0 {
		return 0, fmt.Errorf("function pointer is nil")
	}
	return C.uintptr_t(ptr), nil
}
