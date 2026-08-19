package certificate

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

func LoadOrCreate(dir string) (tls.Certificate, string, error) {
	certPath := filepath.Join(dir, "max-proxy-ca.crt")
	keyPath := filepath.Join(dir, "max-proxy-ca.key")
	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		cert.Leaf, _ = x509.ParseCertificate(cert.Certificate[0])
		return cert, certPath, nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return tls.Certificate{}, "", err
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tpl := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Max Proxy Mock Local CA", Organization: []string{"Max Proxy Mock"}}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err = os.WriteFile(certPath, certPEM, 0644); err != nil {
		return tls.Certificate{}, "", err
	}
	if err = os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return tls.Certificate{}, "", err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	cert.Leaf, _ = x509.ParseCertificate(der)
	return cert, certPath, nil
}
