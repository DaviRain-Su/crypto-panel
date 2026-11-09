package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func coherentKV(t *testing.T) (*Store, *KV, *KV) {
	t.Helper()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writePlugin(t, src, storeManifest, `export function page() { return {}; }`)
	store := NewStore(filepath.Join(root, "plugins"), nil)
	if _, err := store.Install(src); err != nil {
		t.Fatal(err)
	}
	first, err := store.KV("com.example.store")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewStore(store.Dir(), nil).KV("com.example.store")
	if err != nil {
		t.Fatal(err)
	}
	return store, first, second
}

func TestKVIndependentRunnersPreserveUpdates(t *testing.T) {
	_, ui, api := coherentKV(t)
	if _, ok := ui.Get("api"); ok {
		t.Fatal("存储应从空状态开始")
	}
	if err := api.Put("api", []byte(`1`)); err != nil {
		t.Fatal(err)
	}
	if got, ok := ui.Get("api"); !ok || string(got) != "1" {
		t.Fatalf("界面没有看到 API 的更新：%s, %v", got, ok)
	}
	if err := ui.Put("ui", []byte(`2`)); err != nil {
		t.Fatal(err)
	}
	if got, ok := api.Get("api"); !ok || string(got) != "1" {
		t.Fatalf("界面写入覆盖了 API 的数据：%s, %v", got, ok)
	}
	if err := api.Delete("api"); err != nil {
		t.Fatal(err)
	}
	if _, ok := ui.Get("api"); ok {
		t.Fatal("另一个实例仍然读到已删除的数据")
	}
	if got, ok := api.Get("ui"); !ok || string(got) != "2" {
		t.Fatalf("删除覆盖了其他实例写入的数据：%s, %v", got, ok)
	}
}

func TestKVIndependentInstancesCoordinateWrites(t *testing.T) {
	store, _, _ := coherentKV(t)
	const count = 24
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		kv, err := NewStore(store.Dir(), nil).KV("com.example.store")
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := kv.Put(fmt.Sprintf("k%d", i), []byte(`1`)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	kv, err := store.KV("com.example.store")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(kv.Keys()); got != count {
		t.Fatalf("并发写入丢失：保留 %d 个键，期望 %d", got, count)
	}
}

func TestKVCorruptFileIsNotOverwritten(t *testing.T) {
	_, kv, _ := coherentKV(t)
	if err := os.WriteFile(kv.path, []byte(`{"damaged":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := kv.Put("new", []byte(`1`)); err == nil {
		t.Fatal("不能静默覆盖无法读取的现有存储")
	}
	got, err := os.ReadFile(kv.path)
	if err != nil || string(got) != `{"damaged":` {
		t.Fatalf("损坏的原始资料被覆盖：%q, %v", got, err)
	}
}
