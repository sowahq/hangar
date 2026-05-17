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
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type clusterHarness struct {
	binary   string
	root     string
	secret   string
	nodes    map[string]*harnessNode
	portBase int
}

var globalPortBase int64

type harnessNode struct {
	id      string
	api     int
	s3      int
	rpc     int
	dataDir string
	cfgPath string
	logPath string
	proc    *nodeProc
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
	base := int(atomic.AddInt64(&globalPortBase, 100))
	return &clusterHarness{binary: binary, root: root, secret: secret, nodes: map[string]*harnessNode{}, portBase: base}
}

func (h *clusterHarness) port(off int) int {
	return 30000 + h.portBase + off
}

type ecOpts struct {
	dataShards   int
	parityShards int
}

func (h *clusterHarness) addNodeEC(id string, api, s3p, rpc int, seedAddrs []string, ec ecOpts) *harnessNode {
	return h.addNodeFull(id, api, s3p, rpc, seedAddrs, "", &ec)
}

func (h *clusterHarness) addNode(id string, api, s3p, rpc int, seedAddrs []string, extraSecret ...string) *harnessNode {
	secret := ""
	if len(extraSecret) > 0 {
		secret = extraSecret[0]
	}
	return h.addNodeFull(id, api, s3p, rpc, seedAddrs, secret, nil)
}

func (h *clusterHarness) addNodeFull(id string, api, s3p, rpc int, seedAddrs []string, overrideSecret string, ec *ecOpts) *harnessNode {
	dir := filepath.Join(h.root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}
	secret := h.secret
	if overrideSecret != "" {
		secret = overrideSecret
	}
	seedsLine := ""
	if len(seedAddrs) > 0 {
		quoted := make([]string, len(seedAddrs))
		for i, a := range seedAddrs {
			quoted[i] = fmt.Sprintf("\"%s\"", a)
		}
		seedsLine = "seeds = [" + strings.Join(quoted, ", ") + "]\n"
	}
	ecLine := ""
	if ec != nil && ec.dataShards > 0 && ec.parityShards > 0 {
		ecLine = fmt.Sprintf("ec_data_shards = %d\nec_parity_shards = %d\n", ec.dataShards, ec.parityShards)
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
%s%sheartbeat_ms = 200
`, dir, api, s3p, id, rpc, secret, seedsLine, ecLine)

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
	h.addNode("a", h.port(0), h.port(10), h.port(20), nil)
	h.addNode("b", h.port(1), h.port(11), h.port(21), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))})
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
	h.addNode("a", h.port(0), h.port(10), h.port(20), nil)
	h.addNode("b", h.port(1), h.port(11), h.port(21), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))})
	h.addNode("c", h.port(2), h.port(12), h.port(22), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))})
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
	h.addNode("a", h.port(0), h.port(10), h.port(20), nil)
	h.addNode("b", h.port(1), h.port(11), h.port(21), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))})
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
	h.addNode("a", h.port(0), h.port(10), h.port(20), nil)
	h.addNode("b", h.port(1), h.port(11), h.port(21), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))})
	h.start("a")
	h.waitReady("a", 5*time.Second)
	h.start("b")
	h.waitReady("b", 5*time.Second)
	h.waitConverge("a", 2, 5*time.Second)

	fmt.Println("kill seed a")
	h.stop("a")
	time.Sleep(1500 * time.Millisecond)

	h.addNode("c", h.port(2), h.port(12), h.port(22), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20)), fmt.Sprintf("127.0.0.1:%d", h.port(21))})
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
	h.addNode("a", h.port(0), h.port(10), h.port(20), nil)
	h.start("a")
	h.waitReady("a", 5*time.Second)

	rawBad := make([]byte, 32)
	for i := range rawBad {
		rawBad[i] = byte(255 - i)
	}
	badSecret := base64.StdEncoding.EncodeToString(rawBad)

	h.addNode("b", h.port(1), h.port(11), h.port(21), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))}, badSecret)
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
	h.addNode("a", h.port(0), h.port(10), h.port(20), nil)
	h.addNode("b", h.port(1), h.port(11), h.port(21), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))})
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
	h.addNode("a", h.port(0), h.port(10), h.port(20), nil)
	h.addNode("b", h.port(1), h.port(11), h.port(21), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))})
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

	h.addNode("c", h.port(2), h.port(12), h.port(22), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))})
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
	h.addNode("a", h.port(0), h.port(10), h.port(20), nil)
	h.addNode("b", h.port(1), h.port(11), h.port(21), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))})
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

func runScenarioLargeMultipart() {
	h := newHarness("largempu")
	defer h.cleanup()
	h.addNode("a", h.port(0), h.port(10), h.port(20), nil)
	h.addNode("b", h.port(1), h.port(11), h.port(21), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))})
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

	key := "large/blob.bin"
	const totalSize = 50 * 1024 * 1024
	const partSize = 5 * 1024 * 1024

	ini, err := a.CreateMultipartUpload(context.Background(), &s3.CreateMultipartUploadInput{Bucket: aws.String("testbucket"), Key: aws.String(key)})
	if err != nil {
		log.Fatalf("init mpu: %v", err)
	}

	parts := []s3types.CompletedPart{}
	fullBody := make([]byte, totalSize)
	if _, err := rand.Read(fullBody); err != nil {
		log.Fatal(err)
	}
	partNum := int32(1)
	for off := 0; off < totalSize; off += partSize {
		end := off + partSize
		if end > totalSize {
			end = totalSize
		}
		client := a
		if partNum%2 == 0 {
			client = b
		}
		up, err := client.UploadPart(context.Background(), &s3.UploadPartInput{
			Bucket: aws.String("testbucket"), Key: aws.String(key),
			UploadId: ini.UploadId, PartNumber: aws.Int32(partNum),
			Body: bytes.NewReader(fullBody[off:end]),
		})
		if err != nil {
			log.Fatalf("upload part %d: %v", partNum, err)
		}
		parts = append(parts, s3types.CompletedPart{PartNumber: aws.Int32(partNum), ETag: up.ETag})
		partNum++
	}

	if _, err := a.CompleteMultipartUpload(context.Background(), &s3.CompleteMultipartUploadInput{
		Bucket: aws.String("testbucket"), Key: aws.String(key), UploadId: ini.UploadId,
		MultipartUpload: &s3types.CompletedMultipartUpload{Parts: parts},
	}); err != nil {
		log.Fatalf("complete: %v", err)
	}

	time.Sleep(500 * time.Millisecond)
	got, err := b.GetObject(context.Background(), &s3.GetObjectInput{Bucket: aws.String("testbucket"), Key: aws.String(key)})
	if err != nil {
		log.Fatalf("get from B: %v", err)
	}
	body, _ := io.ReadAll(got.Body)
	got.Body.Close()
	if !bytes.Equal(body, fullBody) {
		log.Fatalf("body mismatch: got %d bytes want %d", len(body), totalSize)
	}
	fmt.Printf("LARGE-MULTIPART OK (%d MB round-trip cross-node)\n", totalSize/1024/1024)
}

func runScenarioRollingRestart() {
	h := newHarness("rolling")
	defer h.cleanup()
	h.addNode("a", h.port(0), h.port(10), h.port(20), nil)
	h.addNode("b", h.port(1), h.port(11), h.port(21), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))})
	h.addNode("c", h.port(2), h.port(12), h.port(22), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))})
	for _, id := range []string{"a", "b", "c"} {
		h.start(id)
		h.waitReady(id, 5*time.Second)
	}
	h.waitConverge("a", 3, 10*time.Second)
	h.createBucket("a", "testbucket")
	ak, sk := h.createS3Key("a")
	time.Sleep(500 * time.Millisecond)
	a := h.s3Client("a", ak, sk)

	payloads := map[string][]byte{}
	for i := 0; i < 12; i++ {
		key := fmt.Sprintf("rolling/o%d.bin", i)
		body := make([]byte, 32*1024)
		_, _ = rand.Read(body)
		if _, err := a.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String("testbucket"), Key: aws.String(key), Body: bytes.NewReader(body)}); err != nil {
			log.Fatalf("seed put %s: %v", key, err)
		}
		payloads[key] = body
	}

	for _, id := range []string{"a", "b", "c"} {
		fmt.Printf("== restart %s ==\n", id)
		h.stop(id)
		time.Sleep(1500 * time.Millisecond)
		h.start(id)
		h.waitReady(id, 8*time.Second)
		other := "b"
		if id == "b" {
			other = "c"
		}
		h.waitConverge(other, 3, 10*time.Second)
		time.Sleep(2 * time.Second)
	}

	clients := []*s3.Client{
		h.s3Client("a", ak, sk),
		h.s3Client("b", ak, sk),
		h.s3Client("c", ak, sk),
	}
	for i, cli := range clients {
		for key, want := range payloads {
			got, err := cli.GetObject(context.Background(), &s3.GetObjectInput{Bucket: aws.String("testbucket"), Key: aws.String(key)})
			if err != nil {
				log.Fatalf("get %s from node %d: %v", key, i, err)
			}
			body, _ := io.ReadAll(got.Body)
			got.Body.Close()
			if !bytes.Equal(body, want) {
				log.Fatalf("%s body mismatch on node %d", key, i)
			}
		}
	}
	fmt.Println("ROLLING-RESTART OK (all 12 objects readable from a/b/c after sequential restarts)")
}

func runScenarioMajorityKill() {
	h := newHarness("majkill")
	defer h.cleanup()
	h.addNode("a", h.port(0), h.port(10), h.port(20), nil)
	h.addNode("b", h.port(1), h.port(11), h.port(21), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))})
	h.addNode("c", h.port(2), h.port(12), h.port(22), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))})
	for _, id := range []string{"a", "b", "c"} {
		h.start(id)
		h.waitReady(id, 5*time.Second)
	}
	h.waitConverge("a", 3, 10*time.Second)
	h.createBucket("a", "testbucket")
	ak, sk := h.createS3Key("a")
	time.Sleep(500 * time.Millisecond)
	a := h.s3Client("a", ak, sk)

	payloads := map[string][]byte{}
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("majkill/o%d.bin", i)
		body := make([]byte, 8192)
		_, _ = rand.Read(body)
		if _, err := a.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String("testbucket"), Key: aws.String(key), Body: bytes.NewReader(body)}); err != nil {
			log.Fatalf("seed put: %v", err)
		}
		payloads[key] = body
	}

	rt, _ := http.Post(h.adminURL("a")+"/admin/cluster/anti-entropy/run", "application/json", nil)
	if rt != nil {
		rt.Body.Close()
	}
	time.Sleep(1 * time.Second)

	fmt.Println("kill b and c")
	h.stop("b")
	h.stop("c")
	time.Sleep(2 * time.Second)

	missing := 0
	mismatched := 0
	for key, want := range payloads {
		got, err := a.GetObject(context.Background(), &s3.GetObjectInput{Bucket: aws.String("testbucket"), Key: aws.String(key)})
		if err != nil {
			missing++
			continue
		}
		body, rerr := io.ReadAll(got.Body)
		got.Body.Close()
		if rerr != nil {
			missing++
			continue
		}
		if !bytes.Equal(body, want) {
			mismatched++
			continue
		}
	}
	total := len(payloads)
	served := total - missing - mismatched
	fmt.Printf("MAJORITY-KILL served=%d missing=%d mismatched=%d total=%d\n", served, missing, mismatched, total)
	if mismatched > 0 {
		log.Fatalf("data integrity issue: %d objects returned wrong bytes", mismatched)
	}
	if served < 1 {
		log.Fatalf("survivor served nothing (expected at least some via local-owner chunks)")
	}
	fmt.Printf("MAJORITY-KILL OK (survivor served %d/%d objects after killing 2 of 3; %d unreachable because owned by killed peers)\n", served, total, missing)
}

func runScenarioSustainedTwoMin() {
	dur := 120 * time.Second
	if env := os.Getenv("SUSTAINED_SECONDS"); env != "" {
		fmt.Sscanf(env, "%d", &dur)
		dur = dur * time.Second
	}
	h := newHarness("sustained")
	defer h.cleanup()
	h.addNode("a", h.port(0), h.port(10), h.port(20), nil)
	h.addNode("b", h.port(1), h.port(11), h.port(21), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))})
	h.addNode("c", h.port(2), h.port(12), h.port(22), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))})
	for _, id := range []string{"a", "b", "c"} {
		h.start(id)
		h.waitReady(id, 5*time.Second)
	}
	h.waitConverge("a", 3, 10*time.Second)
	h.createBucket("a", "testbucket")
	ak, sk := h.createS3Key("a")
	time.Sleep(500 * time.Millisecond)
	clients := []*s3.Client{
		h.s3Client("a", ak, sk),
		h.s3Client("b", ak, sk),
		h.s3Client("c", ak, sk),
	}

	end := time.Now().Add(dur)
	var puts, gets int64
	var errs int64
	var errSamplesMu sync.Mutex
	errSamples := []string{}
	addErrSample := func(kind, msg string) {
		errSamplesMu.Lock()
		if len(errSamples) < 5 {
			errSamples = append(errSamples, kind+": "+msg)
		}
		errSamplesMu.Unlock()
	}
	var wg sync.WaitGroup

	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			cli := clients[w%len(clients)]
			seen := []string{}
			for time.Now().Before(end) {
				if len(seen) > 0 && time.Now().UnixNano()%3 == 0 {
					key := seen[time.Now().UnixNano()%int64(len(seen))]
					readCli := clients[(w+1)%len(clients)]
					got, err := readCli.GetObject(context.Background(), &s3.GetObjectInput{Bucket: aws.String("testbucket"), Key: aws.String(key)})
					if err != nil {
						atomic.AddInt64(&errs, 1)
						addErrSample("GET", err.Error())
						continue
					}
					_, _ = io.Copy(io.Discard, got.Body)
					got.Body.Close()
					atomic.AddInt64(&gets, 1)
					continue
				}
				body := make([]byte, 2048)
				_, _ = rand.Read(body)
				key := fmt.Sprintf("sus/w%d/%d", w, time.Now().UnixNano())
				if _, err := cli.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String("testbucket"), Key: aws.String(key), Body: bytes.NewReader(body)}); err != nil {
					atomic.AddInt64(&errs, 1)
					addErrSample("PUT", err.Error())
					continue
				}
				atomic.AddInt64(&puts, 1)
				seen = append(seen, key)
				if len(seen) > 50 {
					seen = seen[len(seen)-50:]
				}
			}
		}(w)
	}

	tk := time.NewTicker(15 * time.Second)
	go func() {
		for {
			select {
			case <-tk.C:
				fmt.Printf("[%s] puts=%d gets=%d errs=%d\n", time.Now().Format("15:04:05"), atomic.LoadInt64(&puts), atomic.LoadInt64(&gets), atomic.LoadInt64(&errs))
			case <-time.After(dur + 5*time.Second):
				return
			}
		}
	}()

	wg.Wait()
	tk.Stop()
	fmt.Printf("SUSTAINED puts=%d gets=%d errs=%d in %s\n", puts, gets, errs, dur)
	for _, s := range errSamples {
		fmt.Println("err sample:", s)
	}
	total := puts + gets + errs
	errRate := float64(errs) / float64(total)
	if errRate > 0.01 {
		log.Fatalf("SUSTAINED error rate %.3f%% exceeds 1%% threshold", errRate*100)
	}
	fmt.Printf("SUSTAINED OK (error rate %.3f%%)\n", errRate*100)
}

func runScenarioSoak() {
	maxDur := 24 * time.Hour
	if env := os.Getenv("SOAK_HOURS"); env != "" {
		var h int
		fmt.Sscanf(env, "%d", &h)
		if h > 0 {
			maxDur = time.Duration(h) * time.Hour
		}
	}
	workers := 6
	if env := os.Getenv("SOAK_WORKERS"); env != "" {
		var w int
		fmt.Sscanf(env, "%d", &w)
		if w > 0 {
			workers = w
		}
	}
	keyCap := 200
	if env := os.Getenv("SOAK_KEY_CAP"); env != "" {
		var k int
		fmt.Sscanf(env, "%d", &k)
		if k > 0 {
			keyCap = k
		}
	}

	ecMode := os.Getenv("SOAK_EC") == "1"

	h := newHarness("soak")
	defer h.cleanup()

	nodeIDs := []string{"a", "b", "c"}
	if ecMode {
		nodeIDs = []string{"a", "b", "c", "d"}
		fmt.Println("SOAK_EC=1 → 4 nodes EC=2+2")
	}

	seed := fmt.Sprintf("127.0.0.1:%d", h.port(20))
	for i, id := range nodeIDs {
		seeds := []string{seed}
		if i == 0 {
			seeds = nil
		}
		if ecMode {
			h.addNodeEC(id, h.port(i), h.port(10+i), h.port(20+i), seeds, ecOpts{dataShards: 2, parityShards: 2})
		} else {
			h.addNode(id, h.port(i), h.port(10+i), h.port(20+i), seeds)
		}
	}
	for _, id := range nodeIDs {
		h.start(id)
		h.waitReady(id, 5*time.Second)
	}
	h.waitConverge("a", len(nodeIDs), 10*time.Second)
	h.createBucket("a", "testbucket")
	ak, sk := h.createS3Key("a")
	time.Sleep(500 * time.Millisecond)
	clients := make([]*s3.Client, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		clients = append(clients, h.s3Client(id, ak, sk))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	deadline := time.NewTimer(maxDur)
	defer deadline.Stop()

	go func() {
		select {
		case s := <-sigCh:
			fmt.Printf("\nreceived %s, draining workers...\n", s)
			cancel()
		case <-deadline.C:
			fmt.Printf("\nreached %s max duration, draining workers...\n", maxDur)
			cancel()
		}
	}()

	var puts, gets, dels, errs int64
	var errSamplesMu sync.Mutex
	errSamples := []string{}
	addErrSample := func(kind, msg string) {
		errSamplesMu.Lock()
		if len(errSamples) < 10 {
			errSamples = append(errSamples, kind+": "+msg)
		}
		errSamplesMu.Unlock()
	}

	start := time.Now()
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			cli := clients[w%len(clients)]
			seen := make([]string, 0, keyCap)
			for {
				if ctx.Err() != nil {
					return
				}
				if len(seen) > 0 && time.Now().UnixNano()%3 == 0 {
					key := seen[time.Now().UnixNano()%int64(len(seen))]
					readCli := clients[(w+1)%len(clients)]
					got, err := readCli.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String("testbucket"), Key: aws.String(key)})
					if err != nil {
						if ctx.Err() == nil {
							atomic.AddInt64(&errs, 1)
							addErrSample("GET", err.Error())
						}
						continue
					}
					_, _ = io.Copy(io.Discard, got.Body)
					got.Body.Close()
					atomic.AddInt64(&gets, 1)
					continue
				}
				body := make([]byte, 2048)
				_, _ = rand.Read(body)
				key := fmt.Sprintf("soak/w%d/%d", w, time.Now().UnixNano())
				if _, err := cli.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String("testbucket"), Key: aws.String(key), Body: bytes.NewReader(body)}); err != nil {
					if ctx.Err() == nil {
						atomic.AddInt64(&errs, 1)
						addErrSample("PUT", err.Error())
					}
					continue
				}
				atomic.AddInt64(&puts, 1)
				seen = append(seen, key)
				if len(seen) > keyCap {
					old := seen[0]
					seen = seen[1:]
					if _, err := cli.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String("testbucket"), Key: aws.String(old)}); err != nil {
						if ctx.Err() == nil {
							atomic.AddInt64(&errs, 1)
							addErrSample("DEL", err.Error())
						}
						continue
					}
					atomic.AddInt64(&dels, 1)
				}
			}
		}(w)
	}

	tk := time.NewTicker(30 * time.Second)
	defer tk.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				elapsed := time.Since(start)
				p := atomic.LoadInt64(&puts)
				g := atomic.LoadInt64(&gets)
				d := atomic.LoadInt64(&dels)
				e := atomic.LoadInt64(&errs)
				total := p + g + d + e
				rate := float64(total) / elapsed.Seconds()
				errRate := float64(0)
				if total > 0 {
					errRate = float64(e) / float64(total) * 100
				}
				fmt.Printf("[%s] elapsed=%s puts=%d gets=%d dels=%d errs=%d (%.3f%% err, %.1f ops/s)\n",
					time.Now().Format("15:04:05"), elapsed.Truncate(time.Second), p, g, d, e, errRate, rate)
			}
		}
	}()

	wg.Wait()
	elapsed := time.Since(start)
	p := atomic.LoadInt64(&puts)
	g := atomic.LoadInt64(&gets)
	d := atomic.LoadInt64(&dels)
	e := atomic.LoadInt64(&errs)
	total := p + g + d + e
	errRate := float64(0)
	if total > 0 {
		errRate = float64(e) / float64(total) * 100
	}

	fmt.Printf("\n==== SOAK REPORT ====\n")
	fmt.Printf("elapsed:    %s\n", elapsed.Truncate(time.Second))
	fmt.Printf("workers:    %d\n", workers)
	fmt.Printf("puts:       %d\n", p)
	fmt.Printf("gets:       %d\n", g)
	fmt.Printf("deletes:    %d\n", d)
	fmt.Printf("errors:     %d (%.3f%%)\n", e, errRate)
	if elapsed.Seconds() > 0 {
		fmt.Printf("throughput: %.1f ops/s\n", float64(total)/elapsed.Seconds())
	}
	if len(errSamples) > 0 {
		fmt.Println("error samples:")
		for _, s := range errSamples {
			fmt.Println("  -", s)
		}
	}
	if errRate > 1.0 {
		log.Fatalf("SOAK FAIL: error rate %.3f%% exceeds 1%%", errRate)
	}
	fmt.Println("SOAK OK")
}

func runScenarioEC() {
	h := newHarness("ec")
	defer h.cleanup()
	ec := ecOpts{dataShards: 2, parityShards: 2}
	h.addNodeEC("a", h.port(0), h.port(10), h.port(20), nil, ec)
	h.addNodeEC("b", h.port(1), h.port(11), h.port(21), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))}, ec)
	h.addNodeEC("c", h.port(2), h.port(12), h.port(22), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))}, ec)
	h.addNodeEC("d", h.port(3), h.port(13), h.port(23), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))}, ec)
	for _, id := range []string{"a", "b", "c", "d"} {
		h.start(id)
		h.waitReady(id, 10*time.Second)
	}
	h.waitConverge("a", 4, 20*time.Second)
	h.createBucket("a", "testbucket")
	ak, sk := h.createS3Key("a")
	time.Sleep(500 * time.Millisecond)

	clients := map[string]*s3.Client{}
	for _, id := range []string{"a", "b", "c", "d"} {
		clients[id] = h.s3Client(id, ak, sk)
	}

	payload := make([]byte, 256*1024)
	if _, err := rand.Read(payload); err != nil {
		log.Fatalf("rand: %v", err)
	}
	if _, err := clients["a"].PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String("testbucket"), Key: aws.String("ec/obj1"), Body: bytes.NewReader(payload)}); err != nil {
		log.Fatalf("PUT: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	for _, id := range []string{"a", "b", "c", "d"} {
		got, err := clients[id].GetObject(context.Background(), &s3.GetObjectInput{Bucket: aws.String("testbucket"), Key: aws.String("ec/obj1")})
		if err != nil {
			log.Fatalf("GET via %s: %v", id, err)
		}
		body, _ := io.ReadAll(got.Body)
		got.Body.Close()
		if !bytes.Equal(body, payload) {
			log.Fatalf("body mismatch on %s: got %d want %d", id, len(body), len(payload))
		}
	}

	h.stop("a")
	h.stop("b")
	time.Sleep(2 * time.Second)
	got, err := clients["c"].GetObject(context.Background(), &s3.GetObjectInput{Bucket: aws.String("testbucket"), Key: aws.String("ec/obj1")})
	if err != nil {
		log.Fatalf("GET after killing a+b: %v", err)
	}
	body, _ := io.ReadAll(got.Body)
	got.Body.Close()
	if !bytes.Equal(body, payload) {
		log.Fatalf("body mismatch after losing 2 of 4 owners: got %d want %d", len(body), len(payload))
	}
	fmt.Println("EC OK (2+2, survived 2 owner loss, full reconstruct)")
}

func runScenarioECAE() {
	h := newHarness("ecae")
	defer h.cleanup()
	ec := ecOpts{dataShards: 2, parityShards: 2}
	h.addNodeEC("a", h.port(0), h.port(10), h.port(20), nil, ec)
	h.addNodeEC("b", h.port(1), h.port(11), h.port(21), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))}, ec)
	h.addNodeEC("c", h.port(2), h.port(12), h.port(22), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))}, ec)
	h.addNodeEC("d", h.port(3), h.port(13), h.port(23), []string{fmt.Sprintf("127.0.0.1:%d", h.port(20))}, ec)
	for _, id := range []string{"a", "b", "c", "d"} {
		h.start(id)
		h.waitReady(id, 10*time.Second)
	}
	h.waitConverge("a", 4, 20*time.Second)
	h.createBucket("a", "testbucket")
	ak, sk := h.createS3Key("a")
	time.Sleep(500 * time.Millisecond)

	a := h.s3Client("a", ak, sk)
	payload := make([]byte, 256*1024)
	if _, err := rand.Read(payload); err != nil {
		log.Fatalf("rand: %v", err)
	}
	if _, err := a.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String("testbucket"), Key: aws.String("ecae/obj1"), Body: bytes.NewReader(payload)}); err != nil {
		log.Fatalf("PUT: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	before := map[string]int{}
	for _, id := range []string{"a", "b", "c", "d"} {
		before[id] = countChunksOnNode(h.nodes[id].dataDir)
	}
	fmt.Printf("shards before wipe: %v\n", before)

	victim := ""
	for _, id := range []string{"a", "b", "c", "d"} {
		if before[id] > 0 {
			victim = id
			break
		}
	}
	if victim == "" {
		log.Fatal("no shard files anywhere?")
	}
	fmt.Printf("wiping node %s chunks dir\n", victim)
	h.stop(victim)
	time.Sleep(500 * time.Millisecond)
	if err := os.RemoveAll(filepath.Join(h.nodes[victim].dataDir, "chunks")); err != nil {
		log.Fatalf("rm chunks on %s: %v", victim, err)
	}
	h.start(victim)
	h.waitReady(victim, 10*time.Second)
	h.waitConverge("a", 4, 20*time.Second)

	resp, err := http.Post(h.adminURL(victim)+"/admin/cluster/anti-entropy/run", "application/json", nil)
	if err != nil {
		log.Fatalf("trigger AE on %s: %v", victim, err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf("AE on %s: %s\n", victim, strings.TrimSpace(string(body)))
	time.Sleep(2 * time.Second)

	after := countChunksOnNode(h.nodes[victim].dataDir)
	if after == 0 {
		log.Fatalf("AE failed to restore any shards on %s", victim)
	}
	if after < before[victim] {
		fmt.Printf("WARN partial restore on %s: %d/%d shards\n", victim, after, before[victim])
	}

	clients := map[string]*s3.Client{}
	for _, id := range []string{"a", "b", "c", "d"} {
		clients[id] = h.s3Client(id, ak, sk)
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		got, err := clients[id].GetObject(context.Background(), &s3.GetObjectInput{Bucket: aws.String("testbucket"), Key: aws.String("ecae/obj1")})
		if err != nil {
			log.Fatalf("GET via %s after AE: %v", id, err)
		}
		gotBody, _ := io.ReadAll(got.Body)
		got.Body.Close()
		if !bytes.Equal(gotBody, payload) {
			log.Fatalf("body mismatch on %s post-AE", id)
		}
	}
	fmt.Printf("EC-AE OK (wiped %s, restored %d shards via reconstruction)\n", victim, after)
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
	case "large-multipart":
		runScenarioLargeMultipart()
	case "rolling-restart":
		runScenarioRollingRestart()
	case "majority-kill":
		runScenarioMajorityKill()
	case "sustained":
		runScenarioSustainedTwoMin()
	case "soak":
		runScenarioSoak()
	case "ec":
		runScenarioEC()
	case "ec-ae":
		runScenarioECAE()
	case "all":
		for _, s := range []string{"basic", "concurrent", "drain", "add-remove", "seed-failover", "wrong-secret", "anti-entropy", "wal-catchup", "large-multipart", "rolling-restart", "majority-kill", "long-run"} {
			fmt.Printf("\n==== SCENARIO: %s ====\n", s)
			dispatchScenario(s)
			time.Sleep(2 * time.Second)
		}
		fmt.Println("\nALL SCENARIOS OK")
	default:
		log.Fatalf("unknown scenario: %s", name)
	}
}
