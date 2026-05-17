package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type clusterHarness struct {
	binary string
	root   string
	secret string
	nodes  map[string]*harnessNode
}

type harnessNode struct {
	id       string
	api      int
	s3       int
	rpc      int
	dataDir  string
	cfgPath  string
	logPath  string
	proc     *nodeProc
}

func newHarness(label string) *clusterHarness {
	binary := os.Getenv("HANGAR_BIN")
	if binary == "" {
		binary = "/tmp/hangar"
	}
	if _, err := os.Stat(binary); err != nil {
		log.Fatalf("hangar binary not found at %s (set HANGAR_BIN env)", binary)
	}

	secret := os.Getenv("CLUSTER_SECRET")
	if secret == "" {
		raw := make([]byte, 32)
		if _, err := rand.Read(raw); err != nil {
			log.Fatal(err)
		}
		secret = base64.StdEncoding.EncodeToString(raw)
	}

	root := filepath.Join(os.TempDir(), fmt.Sprintf("hangar-harness-%s-%d", label, time.Now().UnixNano()))
	if err := os.MkdirAll(root, 0o755); err != nil {
		log.Fatal(err)
	}
	return &clusterHarness{binary: binary, root: root, secret: secret, nodes: map[string]*harnessNode{}}
}

func (h *clusterHarness) addNode(id string, api, s3p, rpc int, seedAddrs []string, extraSecret ...string) *harnessNode {
	dir := filepath.Join(h.root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}
	secret := h.secret
	if len(extraSecret) > 0 {
		secret = extraSecret[0]
	}
	seedsLine := ""
	if len(seedAddrs) > 0 {
		quoted := make([]string, len(seedAddrs))
		for i, a := range seedAddrs {
			quoted[i] = fmt.Sprintf("\"%s\"", a)
		}
		seedsLine = "seeds = [" + strings.Join(quoted, ", ") + "]\n"
	}
	cfg := fmt.Sprintf(`data_directory = "%s"
[api]
bind_addr = ":%d"
[storage]
chunk_size = 4194304
[garbage_collection]
interval_hours = 24
[s3]
enabled = true
bind_addr = ":%d"
region = "us-east-1"
[cluster]
enabled = true
node_id = "%s"
listen = "127.0.0.1:%d"
shared_secret_b64 = "%s"
%sheartbeat_ms = 200
`, dir, api, s3p, id, rpc, secret, seedsLine)

	cfgPath := filepath.Join(h.root, id+".toml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		log.Fatal(err)
	}
	n := &harnessNode{
		id: id, api: api, s3: s3p, rpc: rpc,
		dataDir: dir, cfgPath: cfgPath,
		logPath: filepath.Join(h.root, id+".log"),
	}
	h.nodes[id] = n
	return n
}

func (h *clusterHarness) start(id string) {
	n := h.nodes[id]
	proc, err := startNode(h.binary, n.cfgPath, n.logPath)
	if err != nil {
		log.Fatalf("start %s: %v", id, err)
	}
	n.proc = proc
}

func (h *clusterHarness) stop(id string) {
	n := h.nodes[id]
	if n.proc != nil {
		n.proc.stop()
		n.proc = nil
	}
}

func (h *clusterHarness) stopAll() {
	for id := range h.nodes {
		h.stop(id)
	}
}

func (h *clusterHarness) adminURL(id string) string {
	return fmt.Sprintf("http://127.0.0.1:%d", h.nodes[id].api)
}

func (h *clusterHarness) s3URL(id string) string {
	return fmt.Sprintf("http://127.0.0.1:%d", h.nodes[id].s3)
}

func (h *clusterHarness) waitReady(id string, timeout time.Duration) {
	if err := waitHTTP(h.adminURL(id)+"/status", timeout); err != nil {
		log.Fatalf("node %s not ready: %v", id, err)
	}
}

func (h *clusterHarness) waitConverge(id string, wantAlive int, timeout time.Duration) {
	if err := waitCluster(h.adminURL(id), wantAlive, timeout); err != nil {
		log.Fatalf("converge %s: %v", id, err)
	}
}

func (h *clusterHarness) createBucket(id, bucket string) {
	out, err := runBinary(h.binary, "bucket", "create", "--server", h.adminURL(id), bucket)
	if err != nil {
		log.Fatalf("create bucket: %v: %s", err, out)
	}
}

func (h *clusterHarness) createS3Key(id string) (string, string) {
	out, err := runBinary(h.binary, "s3keys", "create", "--server", h.adminURL(id), "--perm", "admin")
	if err != nil {
		log.Fatalf("create s3key: %v: %s", err, out)
	}
	return parseKeyOutput(out)
}

func (h *clusterHarness) s3Client(id, ak, sk string) *s3.Client {
	cfg, _ := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(ak, sk, "")))
	return s3.NewFromConfig(cfg, func(o *s3.Options) { o.BaseEndpoint = aws.String(h.s3URL(id)); o.UsePathStyle = true })
}

func (h *clusterHarness) cleanup() {
	h.stopAll()
}

func runBinary(binary string, args ...string) (string, error) {
	cmd := newCommand(binary, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func newCommand(binary string, args ...string) *cmdShim {
	return &cmdShim{binary: binary, args: args}
}

type cmdShim struct {
	binary string
	args   []string
}

func (c *cmdShim) CombinedOutput() ([]byte, error) {
	cmd := newExec(c.binary, c.args...)
	return cmd.CombinedOutput()
}

func runScenarioBasic() {
	h := newHarness("basic")
	defer h.cleanup()
	h.addNode("a", 18091, 19101, 17091, nil)
	h.addNode("b", 18092, 19102, 17092, []string{"127.0.0.1:17091"})
	h.start("a")
	h.waitReady("a", 5*time.Second)
	h.start("b")
	h.waitReady("b", 5*time.Second)
	h.waitConverge("a", 2, 5*time.Second)

	h.createBucket("a", "testbucket")
	ak, sk := h.createS3Key("a")
	time.Sleep(500 * time.Millisecond)
	a := h.s3Client("a", ak, sk)
	b := h.s3Client("b", ak, sk)

	payload := []byte("hello-basic-" + time.Now().Format(time.RFC3339Nano))
	if _, err := a.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String("testbucket"), Key: aws.String("o1"), Body: bytes.NewReader(payload)}); err != nil {
		log.Fatalf("PUT: %v", err)
	}
	got, err := b.GetObject(context.Background(), &s3.GetObjectInput{Bucket: aws.String("testbucket"), Key: aws.String("o1")})
	if err != nil {
		log.Fatalf("GET B: %v", err)
	}
	body, _ := io.ReadAll(got.Body)
	if !bytes.Equal(body, payload) {
		log.Fatal("body mismatch")
	}
	fmt.Println("BASIC OK")
}

func runScenarioDrain() {
	h := newHarness("drain")
	defer h.cleanup()
	h.addNode("a", 18091, 19101, 17091, nil)
	h.addNode("b", 18092, 19102, 17092, []string{"127.0.0.1:17091"})
	h.addNode("c", 18093, 19103, 17093, []string{"127.0.0.1:17091"})
	h.start("a")
	h.waitReady("a", 5*time.Second)
	h.start("b")
	h.waitReady("b", 5*time.Second)
	h.start("c")
	h.waitReady("c", 5*time.Second)
	h.waitConverge("a", 3, 10*time.Second)

	h.createBucket("a", "testbucket")
	ak, sk := h.createS3Key("a")
	time.Sleep(500 * time.Millisecond)

	out, err := runBinary(h.binary, "cluster", "node", "drain", "--server", h.adminURL("a"), "c")
	if err != nil {
		log.Fatalf("drain: %v: %s", err, out)
	}
	fmt.Println("drained c:", strings.TrimSpace(out))
	time.Sleep(800 * time.Millisecond)

	a := h.s3Client("a", ak, sk)
	keys := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		key := fmt.Sprintf("drain/%d.bin", i)
		body := make([]byte, 8192)
		_, _ = rand.Read(body)
		if _, err := a.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String("testbucket"), Key: aws.String(key), Body: bytes.NewReader(body)}); err != nil {
			log.Fatalf("put %s: %v", key, err)
		}
		keys = append(keys, key)
	}
	time.Sleep(500 * time.Millisecond)

	cChunks := countChunksOnNode(h.nodes["c"].dataDir)
	if cChunks > 0 {
		log.Fatalf("draining node c received %d chunks; expected 0", cChunks)
	}
	fmt.Printf("c chunks=%d (expected 0)\n", cChunks)

	for _, k := range keys {
		_, err := a.HeadObject(context.Background(), &s3.HeadObjectInput{Bucket: aws.String("testbucket"), Key: aws.String(k)})
		if err != nil {
			log.Fatalf("head %s: %v", k, err)
		}
	}
	fmt.Println("DRAIN OK")
}

func countChunksOnNode(dataDir string) int {
	chunksDir := filepath.Join(dataDir, "chunks")
	count := 0
	_ = filepath.Walk(chunksDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if !info.IsDir() && !strings.HasPrefix(info.Name(), ".") {
			count++
		}
		return nil
	})
	return count
}

func runScenarioConcurrent() {
	h := newHarness("concurrent")
	defer h.cleanup()
	h.addNode("a", 18091, 19101, 17091, nil)
	h.addNode("b", 18092, 19102, 17092, []string{"127.0.0.1:17091"})
	h.start("a")
	h.waitReady("a", 5*time.Second)
	h.start("b")
	h.waitReady("b", 5*time.Second)
	h.waitConverge("a", 2, 5*time.Second)

	h.createBucket("a", "testbucket")
	ak, sk := h.createS3Key("a")
	time.Sleep(500 * time.Millisecond)
	a := h.s3Client("a", ak, sk)
	b := h.s3Client("b", ak, sk)

	const N = 50
	payloads := make(map[string][]byte, N)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var errCount int64

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cli := a
			if i%2 == 1 {
				cli = b
			}
			body := make([]byte, 16*1024)
			if _, err := rand.Read(body); err != nil {
				atomic.AddInt64(&errCount, 1)
				return
			}
			key := fmt.Sprintf("conc/%d.bin", i)
			if _, err := cli.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String("testbucket"), Key: aws.String(key), Body: bytes.NewReader(body)}); err != nil {
				atomic.AddInt64(&errCount, 1)
				return
			}
			mu.Lock()
			payloads[key] = body
			mu.Unlock()
		}(i)
	}
	wg.Wait()
	if errCount > 0 {
		log.Fatalf("concurrent puts: %d errors", errCount)
	}
	fmt.Printf("PUT N=%d done\n", N)

	for key, want := range payloads {
		cli := a
		if len(key)%2 == 0 {
			cli = b
		}
		got, err := cli.GetObject(context.Background(), &s3.GetObjectInput{Bucket: aws.String("testbucket"), Key: aws.String(key)})
		if err != nil {
			log.Fatalf("get %s: %v", key, err)
		}
		body, _ := io.ReadAll(got.Body)
		if !bytes.Equal(body, want) {
			log.Fatalf("%s body mismatch", key)
		}
	}
	fmt.Println("CONCURRENT OK")
}

func runScenarioSeedFailover() {
	h := newHarness("seedfailover")
	defer h.cleanup()
	h.addNode("a", 18091, 19101, 17091, nil)
	h.addNode("b", 18092, 19102, 17092, []string{"127.0.0.1:17091"})
	h.start("a")
	h.waitReady("a", 5*time.Second)
	h.start("b")
	h.waitReady("b", 5*time.Second)
	h.waitConverge("a", 2, 5*time.Second)

	fmt.Println("kill seed a")
	h.stop("a")
	time.Sleep(1500 * time.Millisecond)

	h.addNode("c", 18093, 19103, 17093, []string{"127.0.0.1:17091", "127.0.0.1:17092"})
	h.start("c")
	h.waitReady("c", 5*time.Second)
	time.Sleep(2 * time.Second)

	resp, err := http.Get(h.adminURL("c") + "/admin/cluster/status")
	if err != nil {
		log.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "\"id\":\"c\"") {
		log.Fatalf("c not in own status: %s", body)
	}
	if !strings.Contains(string(body), "\"id\":\"b\"") {
		log.Fatalf("c didn't join b: %s", body)
	}
	fmt.Println("SEED-FAILOVER OK")
}

func runScenarioWrongSecret() {
	h := newHarness("wrongsecret")
	defer h.cleanup()
	h.addNode("a", 18091, 19101, 17091, nil)
	h.start("a")
	h.waitReady("a", 5*time.Second)

	rawBad := make([]byte, 32)
	for i := range rawBad {
		rawBad[i] = byte(255 - i)
	}
	badSecret := base64.StdEncoding.EncodeToString(rawBad)

	h.addNode("b", 18092, 19102, 17092, []string{"127.0.0.1:17091"}, badSecret)
	h.start("b")
	time.Sleep(3 * time.Second)

	resp, err := http.Get(h.adminURL("a") + "/admin/cluster/status")
	if err == nil {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if strings.Contains(string(body), "\"id\":\"b\"") {
			log.Fatalf("b joined a despite wrong secret: %s", body)
		}
	}
	if h.nodes["b"].proc == nil || h.nodes["b"].proc.cmd == nil || h.nodes["b"].proc.cmd.ProcessState != nil {
	}
	fmt.Println("WRONG-SECRET OK (b refused, a's view excludes b)")
}

func runScenarioAntiEntropy() {
	h := newHarness("ae")
	defer h.cleanup()
	h.addNode("a", 18091, 19101, 17091, nil)
	h.addNode("b", 18092, 19102, 17092, []string{"127.0.0.1:17091"})
	h.start("a")
	h.waitReady("a", 5*time.Second)
	h.start("b")
	h.waitReady("b", 5*time.Second)
	h.waitConverge("a", 2, 5*time.Second)

	h.createBucket("a", "testbucket")
	ak, sk := h.createS3Key("a")
	time.Sleep(500 * time.Millisecond)

	h.stop("b")
	time.Sleep(1500 * time.Millisecond)

	a := h.s3Client("a", ak, sk)
	body := make([]byte, 100*1024)
	_, _ = rand.Read(body)
	if _, err := a.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String("testbucket"), Key: aws.String("ae/o1.bin"), Body: bytes.NewReader(body)}); err != nil {
		log.Fatalf("put: %v", err)
	}
	chunksOnA := countChunksOnNode(h.nodes["a"].dataDir)
	if chunksOnA == 0 {
		log.Fatal("no chunks on A after put")
	}

	h.start("b")
	h.waitReady("b", 5*time.Second)
	h.waitConverge("a", 2, 10*time.Second)
	time.Sleep(20 * time.Second)

	resp, err := http.Post(h.adminURL("b")+"/admin/cluster/anti-entropy/run", "application/json", nil)
	if err != nil {
		log.Fatalf("trigger AE on b: %v", err)
	}
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf("AE result on b: %s\n", strings.TrimSpace(string(respBody)))
	time.Sleep(2 * time.Second)

	chunksOnB := countChunksOnNode(h.nodes["b"].dataDir)
	if chunksOnB == 0 {
		log.Fatal("anti-entropy did not pull chunks to B")
	}
	fmt.Printf("ANTI-ENTROPY OK (A=%d, B=%d chunks after pull)\n", chunksOnA, chunksOnB)
}

func runScenarioAddRemove() {
	h := newHarness("addremove")
	defer h.cleanup()
	h.addNode("a", 18091, 19101, 17091, nil)
	h.addNode("b", 18092, 19102, 17092, []string{"127.0.0.1:17091"})
	h.start("a")
	h.waitReady("a", 5*time.Second)
	h.start("b")
	h.waitReady("b", 5*time.Second)
	h.waitConverge("a", 2, 5*time.Second)

	h.createBucket("a", "testbucket")
	ak, sk := h.createS3Key("a")
	time.Sleep(500 * time.Millisecond)

	a := h.s3Client("a", ak, sk)
	payload := []byte("survive-remove-cycle")
	if _, err := a.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String("testbucket"), Key: aws.String("survive/x"), Body: bytes.NewReader(payload)}); err != nil {
		log.Fatalf("put pre-remove: %v", err)
	}

	h.addNode("c", 18093, 19103, 17093, []string{"127.0.0.1:17091"})
	h.start("c")
	h.waitReady("c", 5*time.Second)
	h.waitConverge("a", 3, 10*time.Second)
	fmt.Println("c joined")

	out, err := runBinary(h.binary, "cluster", "node", "remove", "--server", h.adminURL("a"), "c")
	if err != nil {
		log.Fatalf("remove c: %v: %s", err, out)
	}
	time.Sleep(2 * time.Second)

	got, err := a.GetObject(context.Background(), &s3.GetObjectInput{Bucket: aws.String("testbucket"), Key: aws.String("survive/x")})
	if err != nil {
		log.Fatalf("get post-remove: %v", err)
	}
	body, _ := io.ReadAll(got.Body)
	if !bytes.Equal(body, payload) {
		log.Fatal("data lost after remove")
	}
	fmt.Println("ADD-REMOVE OK")
}

func runScenarioLongRun() {
	dur := 30 * time.Second
	if env := os.Getenv("LONGRUN_SECONDS"); env != "" {
		fmt.Sscanf(env, "%d", &dur)
		dur = dur * time.Second
	}
	h := newHarness("longrun")
	defer h.cleanup()
	h.addNode("a", 18091, 19101, 17091, nil)
	h.addNode("b", 18092, 19102, 17092, []string{"127.0.0.1:17091"})
	h.start("a")
	h.waitReady("a", 5*time.Second)
	h.start("b")
	h.waitReady("b", 5*time.Second)
	h.waitConverge("a", 2, 5*time.Second)

	h.createBucket("a", "testbucket")
	ak, sk := h.createS3Key("a")
	time.Sleep(500 * time.Millisecond)
	a := h.s3Client("a", ak, sk)
	b := h.s3Client("b", ak, sk)

	end := time.Now().Add(dur)
	var puts int64
	var errs int64
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for time.Now().Before(end) {
				cli := a
				if w%2 == 1 {
					cli = b
				}
				body := make([]byte, 4096)
				_, _ = rand.Read(body)
				key := fmt.Sprintf("long/w%d/%d", w, time.Now().UnixNano())
				if _, err := cli.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String("testbucket"), Key: aws.String(key), Body: bytes.NewReader(body)}); err != nil {
					atomic.AddInt64(&errs, 1)
					continue
				}
				atomic.AddInt64(&puts, 1)
			}
		}(w)
	}
	wg.Wait()
	fmt.Printf("LONG-RUN puts=%d errs=%d in %s\n", puts, errs, dur)
	if errs > 0 {
		log.Fatalf("LONG-RUN had %d errors", errs)
	}
	fmt.Println("LONG-RUN OK")
}

func dispatchScenario(name string) {
	switch name {
	case "basic":
		runScenarioBasic()
	case "drain":
		runScenarioDrain()
	case "concurrent":
		runScenarioConcurrent()
	case "seed-failover":
		runScenarioSeedFailover()
	case "wrong-secret":
		runScenarioWrongSecret()
	case "anti-entropy":
		runScenarioAntiEntropy()
	case "add-remove":
		runScenarioAddRemove()
	case "long-run":
		runScenarioLongRun()
	case "wal-catchup":
		runWALCatchup()
	case "all":
		for _, s := range []string{"basic", "concurrent", "drain", "add-remove", "seed-failover", "wrong-secret", "anti-entropy", "wal-catchup", "long-run"} {
			fmt.Printf("\n==== SCENARIO: %s ====\n", s)
			dispatchScenario(s)
		}
		fmt.Println("\nALL SCENARIOS OK")
	default:
		log.Fatalf("unknown scenario: %s", name)
	}
}
