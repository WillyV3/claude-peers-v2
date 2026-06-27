package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
)

func defaultBaseURL() string {
	if u := os.Getenv("CPV2_URL"); u != "" {
		return u
	}
	return "http://127.0.0.1:7900"
}

// clientSend POSTs a message to the broker. A non-nil err means a transport
// failure; status reflects the broker's HTTP response (e.g. 403 not paired).
func clientSend(base, from, to string, mode DeliverMode, content string) (status int, body []byte, err error) {
	if mode == "" {
		mode = DeliverSteer
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(map[string]any{
		"from":      from,
		"to":        to,
		"content":   content,
		"deliverAs": mode,
	}); err != nil {
		return 0, nil, err
	}
	resp, err := http.Post(base+"/send", "application/json", &buf)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, data, nil
}

// clientPair requests a pairing code from the broker.
func clientPair(base, from, to string) (code string, status int, err error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(map[string]string{"from": from, "to": to}); err != nil {
		return "", 0, err
	}
	resp, err := http.Post(base+"/pair", "application/json", &buf)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", resp.StatusCode, err
	}
	c, _ := out["code"].(string)
	return c, resp.StatusCode, nil
}

// clientApprove approves a pending pairing.
func clientApprove(base, owner, code string) (status int, body []byte, err error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(map[string]string{"owner": owner, "code": code}); err != nil {
		return 0, nil, err
	}
	resp, err := http.Post(base+"/pair/approve", "application/json", &buf)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, data, nil
}

// clientPeers lists registered peers from the broker.
func clientPeers(base string) ([]Peer, int, error) {
	resp, err := http.Get(base + "/peers")
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	var peers []Peer
	if err := json.NewDecoder(resp.Body).Decode(&peers); err != nil {
		return nil, resp.StatusCode, err
	}
	return peers, resp.StatusCode, nil
}

func cmdSend(args []string) int {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	from := fs.String("from", "", "sender agent")
	to := fs.String("to", "", "recipient agent")
	mode := fs.String("mode", string(DeliverSteer), "deliver mode: steer|followUp|nextTurn")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	content := strings.Join(fs.Args(), " ")
	if *from == "" || *to == "" || content == "" {
		fmt.Fprintln(os.Stderr, "usage: cpv2 send --from <a> --to <b> [--mode steer|followUp|nextTurn] <content...>")
		return 2
	}
	status, body, err := clientSend(defaultBaseURL(), *from, *to, DeliverMode(*mode), content)
	if err != nil {
		fmt.Fprintln(os.Stderr, "send:", err)
		return 1
	}
	fmt.Println(string(body))
	if status != http.StatusOK {
		return 1
	}
	return 0
}

func cmdPair(args []string) int {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	from := fs.String("from", "", "sender agent (requester)")
	to := fs.String("to", "", "owner agent (target)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *from == "" || *to == "" {
		fmt.Fprintln(os.Stderr, "usage: cpv2 pair --from <a> --to <b>")
		return 2
	}
	code, _, err := clientPair(defaultBaseURL(), *from, *to)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pair:", err)
		return 1
	}
	fmt.Println(code)
	return 0
}

func cmdApprove(args []string) int {
	fs := flag.NewFlagSet("approve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	owner := fs.String("owner", "", "owner agent")
	code := fs.String("code", "", "pairing code")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *owner == "" || *code == "" {
		fmt.Fprintln(os.Stderr, "usage: cpv2 approve --owner <b> --code <c>")
		return 2
	}
	status, body, err := clientApprove(defaultBaseURL(), *owner, *code)
	if err != nil {
		fmt.Fprintln(os.Stderr, "approve:", err)
		return 1
	}
	fmt.Println(string(body))
	if status != http.StatusOK {
		return 1
	}
	return 0
}

func cmdPeers(args []string) int {
	fs := flag.NewFlagSet("peers", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	peers, status, err := clientPeers(defaultBaseURL())
	if err != nil {
		fmt.Fprintln(os.Stderr, "peers:", err)
		return 1
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT\tONLINE\tMACHINE\tCWD")
	for _, p := range peers {
		online := "no"
		if p.Online {
			online = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Agent, online, p.Machine, p.Cwd)
	}
	w.Flush()
	if status != http.StatusOK {
		return 1
	}
	return 0
}
