package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

type startupReceipt struct {
	LaunchID             string `json:"launchId"`
	InstanceID           string `json:"instanceId"`
	ListenAddress        string `json:"listenAddress"`
	CertificateDERBase64 string `json:"certificateDerBase64,omitempty"`
}

var launchIdentity = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

type startupListener struct {
	net.Listener
	receiptPath string
	receipt     startupReceipt
}

// Optional host supervision stays in the process boundary. Domain/service
// packages do not know whether the host is Android, a desktop launcher or NAS.
func openStartupListener(address, receiptPath, launchID string, ephemeralTLS bool) (*startupListener, error) {
	if (receiptPath == "") != (launchID == "") {
		return nil, errors.New("startup receipt and launch identity must be configured together")
	}
	if receiptPath != "" && (!filepath.IsAbs(receiptPath) || !launchIdentity.MatchString(launchID)) {
		return nil, errors.New("invalid startup receipt configuration")
	}
	if ephemeralTLS && receiptPath == "" {
		return nil, errors.New("ephemeral TLS requires a private startup receipt")
	}
	listener, e := net.Listen("tcp", address)
	if e != nil {
		return nil, e
	}
	receipt := startupReceipt{LaunchID: launchID, ListenAddress: listener.Addr().String()}
	if ephemeralTLS {
		tcp, ok := listener.Addr().(*net.TCPAddr)
		if !ok || !tcp.IP.IsLoopback() {
			listener.Close()
			return nil, errors.New("ephemeral TLS is restricted to a local supervised listener")
		}
		certificate, der, err := ephemeralCertificate()
		if err != nil {
			listener.Close()
			return nil, err
		}
		receipt.CertificateDERBase64 = base64.StdEncoding.EncodeToString(der)
		listener = tls.NewListener(listener, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
	}
	return &startupListener{Listener: listener, receiptPath: receiptPath, receipt: receipt}, nil
}

func (l *startupListener) publishReceipt(instanceID string) error {
	if l.receiptPath == "" {
		return nil
	}
	receipt := l.receipt
	receipt.InstanceID = instanceID
	return publishStartupReceipt(l.receiptPath, receipt)
}

func ephemeralCertificate() (tls.Certificate, []byte, error) {
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return tls.Certificate{}, nil, e
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		return tls.Certificate{}, nil, e
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Qimeng supervised local service"}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback}, DNSNames: []string{"localhost"}}
	der, e := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if e != nil {
		return tls.Certificate{}, nil, e
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, der, nil
}

func publishStartupReceipt(filename string, value startupReceipt) error {
	// The trusted host chooses the private directory. File operations are scoped
	// through os.Root; a symlink at the final name is replaced, never followed.
	root, e := os.OpenRoot(filepath.Dir(filename))
	if e != nil {
		return e
	}
	defer root.Close()
	random := make([]byte, 16)
	if _, e = rand.Read(random); e != nil {
		return e
	}
	temporary := ".startup-" + base64.RawURLEncoding.EncodeToString(random) + ".tmp"
	file, e := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	defer root.Remove(temporary)
	if e = json.NewEncoder(file).Encode(value); e != nil {
		file.Close()
		return e
	}
	if e = file.Sync(); e != nil {
		file.Close()
		return e
	}
	if e = file.Close(); e != nil {
		return e
	}
	return root.Rename(temporary, filepath.Base(filename))
}
