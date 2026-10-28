package config

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const testJSON = `{
  "server": {
    "host": "localhost",
    "port": 8080,
    "tls": {
      "enabled": true,
      "certFile": "/etc/cert.pem"
    },
    "tags": ["web", "api"]
  },
  "database": {
    "connection": {
      "poolSize": 20,
      "readTimeout": 1.5
    },
    "username": "admin"
  }
}`

func mustLoad(t *testing.T, content string) *JsonConfigStore {
	t.Helper()
	store := &JsonConfigStore{}
	store.data = content
	if err := store.loadFromContent(); err != nil {
		t.Fatalf("loadFromContent failed: %v", err)
	}
	return store
}

func TestLoadAndGetNestedKeys(t *testing.T) {
	store := mustLoad(t, testJSON)

	cases := []struct {
		key  string
		want string
	}{
		{"server.host", "localhost"},
		{"server.port", "8080"},
		{"server.tls.enabled", "true"},
		{"server.tls.certFile", "/etc/cert.pem"},
		{"database.connection.poolSize", "20"},
		{"database.connection.readTimeout", "1.5"},
		{"database.username", "admin"},
	}
	for _, c := range cases {
		got, err := store.Get("", c.key)
		if err != nil {
			t.Fatalf("Get(%q) unexpected error: %v", c.key, err)
		}
		if got != c.want {
			t.Errorf("Get(%q) = %q, want %q", c.key, got, c.want)
		}
	}
}

func TestSectionParameterIsIgnored(t *testing.T) {
	store := mustLoad(t, testJSON)

	// 无论传什么 section，读取结果一致（JSON 无 section，全部以 key 传递）
	for _, section := range []string{"", "server", "any", "x.y.z"} {
		got, err := store.Get(section, "server.host")
		if err != nil {
			t.Fatalf("Get section=%q: unexpected error: %v", section, err)
		}
		if got != "localhost" {
			t.Errorf("Get(section=%q) = %q, want %q", section, got, "localhost")
		}
	}
}

func TestGetWithDefault(t *testing.T) {
	store := mustLoad(t, testJSON)

	if got, _ := store.GetWithDefault("", "server.host", "dft"); got != "localhost" {
		t.Errorf("existing key: got %q, want %q", got, "localhost")
	}
	if got, _ := store.GetWithDefault("", "not.exists.key", "dft"); got != "dft" {
		t.Errorf("missing key: got %q, want %q", got, "dft")
	}
}

func TestGetSubtreeReturnsJSON(t *testing.T) {
	store := mustLoad(t, testJSON)

	// 数组节点返回 JSON 文本
	tags, err := store.Get("", "server.tags")
	if err != nil {
		t.Fatalf("Get tags: %v", err)
	}
	if tags != `["api","web"]` && tags != `["web","api"]` {
		t.Errorf("tags = %q, want array json", tags)
	}

	// 对象节点返回 JSON 文本（map 序列化键有序，enabled 在前）
	tls, err := store.Get("", "server.tls")
	if err != nil {
		t.Fatalf("Get tls: %v", err)
	}
	if !strings.Contains(tls, `"certFile"`) || !strings.Contains(tls, `"enabled"`) {
		t.Errorf("tls subtree = %q, want contain certFile/enabled", tls)
	}
}

func TestSetCreatesAndOverwrites(t *testing.T) {
	store := mustLoad(t, testJSON)

	// 覆盖已存在值
	if err := store.Set("", "server.host", "127.0.0.1"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	got, _ := store.Get("", "server.host")
	if got != "127.0.0.1" {
		t.Errorf("overwrite: got %q, want %q", got, "127.0.0.1")
	}

	// 新建深层路径（自动创建中间对象）
	if err := store.Set("", "logging.file.path", "/var/log/app.log"); err != nil {
		t.Fatalf("Set new path failed: %v", err)
	}
	got, _ = store.Get("", "logging.file.path")
	if got != "/var/log/app.log" {
		t.Errorf("new deep key: got %q, want %q", got, "/var/log/app.log")
	}

	// 值类型推断：数字 / bool / 字符串
	_ = store.Set("", "a.b.count", "99")
	if got, _ := store.Get("", "a.b.count"); got != "99" {
		t.Errorf("number: got %q, want %q", got, "99")
	}
	_ = store.Set("", "a.b.enable", "true")
	if got, _ := store.Get("", "a.b.enable"); got != "true" {
		t.Errorf("bool: got %q, want %q", got, "true")
	}
	_ = store.Set("", "a.b.name", "hello")
	if got, _ := store.Get("", "a.b.name"); got != "hello" {
		t.Errorf("string: got %q, want %q", got, "hello")
	}

	// 写入空值也应保留字符串
	_ = store.Set("", "a.b.empty", "")
	if got, _ := store.Get("", "a.b.empty"); got != "" {
		t.Errorf("empty string: got %q", got)
	}
}

func TestDeleteKey(t *testing.T) {
	store := mustLoad(t, testJSON)

	store.DeleteKey("", "server.host")
	if got, _ := store.Get("", "server.host"); got != "" {
		t.Errorf("after delete leaf: got %q, want empty", got)
	}
	// 兄弟节点不受影响
	if got, _ := store.Get("", "server.port"); got != "8080" {
		t.Errorf("sibling key affected: got %q, want %q", got, "8080")
	}

	// 删除整棵子树
	store.DeleteKey("", "database.connection")
	if got, _ := store.Get("", "database.connection.poolSize"); got != "" {
		t.Errorf("after delete subtree: got %q, want empty", got)
	}
	if got, _ := store.Get("", "database.username"); got != "admin" {
		t.Errorf("database.username affected: got %q, want %q", got, "admin")
	}
}

func TestDeleteSection(t *testing.T) {
	store := mustLoad(t, testJSON)

	// JSON 无 section，DeleteSection 按点分路径删除子树
	store.DeleteSection("server")
	if got, _ := store.Get("", "server.host"); got != "" {
		t.Errorf("DeleteSection server: got %q, want empty", got)
	}
	if got, _ := store.Get("", "database.username"); got != "admin" {
		t.Errorf("DeleteSection touched other subtree: got %q, want %q", got, "admin")
	}
}

func TestLoadInvalidContent(t *testing.T) {
	store := &JsonConfigStore{data: "{not valid json"}
	if err := store.loadFromContent(); err == nil {
		t.Fatal("expected error for invalid json")
	}

	// 根节点必须是对象
	store = &JsonConfigStore{data: `["a", "b"]`}
	if err := store.loadFromContent(); err == nil {
		t.Fatal("expected error for non-object root")
	}
}

func TestParserLoad(t *testing.T) {
	dir := t.TempDir()
	filename := createTempJSON(t, dir, testJSON)

	parser := &JsonConfigParser{}
	storage, err := parser.Load(filename)
	if err != nil {
		t.Fatalf("parser.Load: %v", err)
	}

	if got, _ := storage.Get("", "server.host"); got != "localhost" {
		t.Errorf("parser loaded host: got %q, want %q", got, "localhost")
	}
	if got, _ := storage.Get("", "server.port"); got != "8080" {
		t.Errorf("parser loaded port: got %q, want %q", got, "8080")
	}

	// Save 保持最初实现（no-op），调用不应报错
	if err := parser.Save(filename, storage); err != nil {
		t.Fatalf("parser.Save should be no-op: %v", err)
	}
}

// createTempJSON 把内容写入临时文件（与 ini 测试同目录惯例），返回文件名。
func createTempJSON(t *testing.T, dir string, content string) string {
	t.Helper()
	filename := filepath.Join(dir, "input.json")
	if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
		t.Fatalf("write temp json: %v", err)
	}
	return filename
}

// TestConcurrentAccess 并发读写同一 store，验证锁机制下不 panic、最终状态一致。
// 注意：本机 CGO_ENABLED=0 无法使用 -race，此处至少覆盖并发路径的执行正确性。
func TestConcurrentAccess(t *testing.T) {
	// 使用未解析的原始 store（仅填 data），让并发首次访问触发惰性解析，覆盖该路径
	store := &JsonConfigStore{data: testJSON}

	const workers = 32
	const rounds = 200

	var wg sync.WaitGroup
	wg.Add(workers * 3)

	// 读者
	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				if _, err := store.Get("", "server.host"); err != nil {
					t.Errorf("reader %d: %v", id, err)
					return
				}
				store.GetWithDefault("", "not.exists", "dft")
			}
		}(i)
	}

	// 写者（各自独立 key，互不覆盖）
	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				key := "worker.w" + string(rune('a'+id)) + ".counter"
				_ = store.Set("", key, "1")
				_, _ = store.Get("", key)
			}
		}(i)
	}

	// 删除者（对同一中间节点做删除/重建）
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				store.DeleteKey("", "worker.tmp")
				_ = store.Set("", "worker.tmp", "x")
			}
		}()
	}

	wg.Wait()

	// 写者写入的值应全部存在
	for i := 0; i < workers; i++ {
		key := "worker.w" + string(rune('a'+i)) + ".counter"
		if got, _ := store.Get("", key); got != "1" {
			t.Errorf("worker %d counter = %q, want %q", i, got, "1")
		}
	}
}
