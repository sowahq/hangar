package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type nodeProc struct {
	cmd    *exec.Cmd
	cfg    string
	logPath string
}

func startNode(binary, cfg, logPath string) (*nodeProc, error) {
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("log: %w", err)
	}
	cmd := exec.Command(binary, "server", "--config", cfg)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("start: %w", err)
	}
	return &nodeProc{cmd: cmd, cfg: cfg, logPath: logPath}, nil
}

func (n *nodeProc) stop() {
	if n.cmd == nil || n.cmd.Process == nil {
		return
	}
	_ = n.cmd.Process.Kill()
	_ = n.cmd.Wait()
}

func waitHTTP(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", url)
}

func waitCluster(adminURL string, wantAlive int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(adminURL + "/admin/cluster/status")
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			alive := strings.Count(string(body), `"status":"active"`)
			if alive >= wantAlive {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %d active nodes on %s", wantAlive, adminURL)
}

func writeConfig(path string, data string) error {
	return os.WriteFile(path, []byte(data), 0644)
}

func cfgFor(dir, nodeID string, apiPort, s3Port, rpcPort int, seedAddr, secretB64 string) string {
	seedsLine := ""
	if seedAddr != "" {
		seedsLine = fmt.Sprintf("seeds = [\"%s\"]\n", seedAddr)
	}
	return fmt.Sprintf(`data_directory = "%s"
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
`, dir, apiPort, s3Port, nodeID, rpcPort, secretB64, seedsLine)
}

func runWALCatchup() {
	binary := os.Getenv("HANGAR_BIN")
	if binary == "" {
		binary = "/tmp/hangar"
	}
	if _, err := os.Stat(binary); err != nil {
		log.Fatalf("hangar binary not found at %s (set HANGAR_BIN env)", binary)
	}

	secret := os.Getenv("CLUSTER_SECRET")
	if secret == "" {
		log.Fatal("set CLUSTER_SECRET (base64 32 bytes)")
	}

	root := filepath.Join(os.TempDir(), fmt.Sprintf("hangar-walcatchup-%d", time.Now().UnixNano()))
	dirA := filepath.Join(root, "a")
	dirB := filepath.Join(root, "b")
	for _, d := range []string{dirA, dirB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			log.Fatal(err)
		}
	}

	cfgA := filepath.Join(root, "a.toml")
	cfgB := filepath.Join(root, "b.toml")
	if err := writeConfig(cfgA, cfgFor(dirA, "a", 18091, 19101, 17091, "", secret)); err != nil {
		log.Fatal(err)
	}
	if err := writeConfig(cfgB, cfgFor(dirB, "b", 18092, 19102, 17092, "127.0.0.1:17091", secret)); err != nil {
		log.Fatal(err)
	}

	procA, err := startNode(binary, cfgA, filepath.Join(root, "a.log"))
	if err != nil {
		log.Fatal(err)
	}
	defer procA.stop()
	procB, err := startNode(binary, cfgB, filepath.Join(root, "b.log"))
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if procB != nil {
			procB.stop()
		}
	}()

	fmt.Println("== wait for cluster converge ==")
	if err := waitHTTP("http://127.0.0.1:18091/status", 5*time.Second); err != nil {
		log.Fatal(err)
	}
	if err := waitHTTP("http://127.0.0.1:18092/status", 5*time.Second); err != nil {
		log.Fatal(err)
	}
	if err := waitCluster("http://127.0.0.1:18091", 2, 5*time.Second); err != nil {
		log.Fatal(err)
	}
	fmt.Println("converged")

	cmd := exec.Command(binary, "bucket", "create", "--server", "http://127.0.0.1:18091", "walbucket")
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Fatalf("create bucket: %v: %s", err, out)
	}

	out, err := exec.Command(binary, "s3keys", "create", "--server", "http://127.0.0.1:18091", "--perm", "admin").CombinedOutput()
	if err != nil {
		log.Fatalf("s3keys create: %v: %s", err, out)
	}
	ak, sk := parseKeyOutput(string(out))
	fmt.Printf("created s3 key %s\n", ak)
	time.Sleep(800 * time.Millisecond)

	cfgAWS, _ := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(ak, sk, "")))
	cliA := s3.NewFromConfig(cfgAWS, func(o *s3.Options) { o.BaseEndpoint = aws.String("http://127.0.0.1:19101"); o.UsePathStyle = true })
	cliB := s3.NewFromConfig(cfgAWS, func(o *s3.Options) { o.BaseEndpoint = aws.String("http://127.0.0.1:19102"); o.UsePathStyle = true })

	fmt.Println("== kill B ==")
	procB.stop()
	procB = nil
	time.Sleep(1500 * time.Millisecond)

	if err := waitCluster("http://127.0.0.1:18091", 1, 5*time.Second); err != nil {
		log.Fatal(err)
	}
	fmt.Println("A sees B down")

	fmt.Println("== PUT 3 objects on A while B is down ==")
	payloads := map[string]string{}
	for i := 1; i <= 3; i++ {
		key := fmt.Sprintf("wal/obj%d.txt", i)
		p := fmt.Sprintf("wal-catchup-payload-%d-%d", i, time.Now().UnixNano())
		payloads[key] = p
		if _, err := cliA.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String("walbucket"), Key: aws.String(key), Body: bytes.NewReader([]byte(p))}); err != nil {
			log.Fatalf("put %s: %v", key, err)
		}
		fmt.Println("PUT ok", key)
	}

	fmt.Println("== restart B ==")
	procB, err = startNode(binary, cfgB, filepath.Join(root, "b.log"))
	if err != nil {
		log.Fatal(err)
	}
	if err := waitHTTP("http://127.0.0.1:18092/status", 5*time.Second); err != nil {
		log.Fatal(err)
	}
	if err := waitCluster("http://127.0.0.1:18092", 2, 10*time.Second); err != nil {
		log.Fatal(err)
	}
	fmt.Println("B back up; waiting for WAL catchup loop")
	time.Sleep(20 * time.Second)

	fmt.Println("== verify B holds metadata via WAL ==")
	failed := 0
	for key, want := range payloads {
		got, err := cliB.GetObject(context.Background(), &s3.GetObjectInput{Bucket: aws.String("walbucket"), Key: aws.String(key)})
		if err != nil {
			fmt.Printf("[FAIL] GET %s from B: %v\n", key, err)
			failed++
			continue
		}
		body, _ := io.ReadAll(got.Body)
		got.Body.Close()
		if string(body) != want {
			fmt.Printf("[FAIL] %s body mismatch: got %q want %q\n", key, body, want)
			failed++
			continue
		}
		fmt.Printf("ok %s\n", key)
	}

	if failed > 0 {
		log.Fatalf("%d/%d WAL catchup verifications failed", failed, len(payloads))
	}
	fmt.Println("\nWAL CATCHUP OK")
}

func parseKeyOutput(s string) (ak, sk string) {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end < 0 {
		log.Fatalf("cannot parse s3keys output: %s", s)
	}
	body := s[start : end+1]
	akIdx := strings.Index(body, `"access_key_id":`)
	if akIdx < 0 {
		log.Fatalf("no access_key_id in: %s", body)
	}
	rest := body[akIdx+len(`"access_key_id":`):]
	q := strings.Index(rest, `"`)
	if q < 0 {
		log.Fatal("malformed")
	}
	rest = rest[q+1:]
	end2 := strings.Index(rest, `"`)
	ak = rest[:end2]

	skIdx := strings.Index(body, `"secret_key":`)
	rest = body[skIdx+len(`"secret_key":`):]
	q = strings.Index(rest, `"`)
	rest = rest[q+1:]
	end2 = strings.Index(rest, `"`)
	sk = rest[:end2]
	return
}

