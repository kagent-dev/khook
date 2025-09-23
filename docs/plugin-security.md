# Plugin Security Guidelines

This document outlines security best practices for developing and deploying KHook plugins.

## Table of Contents

- [Security Principles](#security-principles)
- [Input Validation](#input-validation)
- [Authentication and Authorization](#authentication-and-authorization)
- [Network Security](#network-security)
- [Data Protection](#data-protection)
- [Resource Management](#resource-management)
- [Secrets Management](#secrets-management)
- [Audit and Logging](#audit-and-logging)
- [Deployment Security](#deployment-security)

## Security Principles

### Principle of Least Privilege

Plugins should operate with minimal required permissions:

```yaml
# RBAC for plugin-specific permissions
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: webhook-plugin-role
  namespace: kagent
rules:
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get", "list"]
  resourceNames: ["webhook-config"]  # Specific resource only
```

### Defense in Depth

Implement multiple layers of security:

1. **Input validation** at plugin boundaries
2. **Authentication** for external connections
3. **Authorization** for resource access
4. **Encryption** for data in transit and at rest
5. **Monitoring** for security events

### Fail Secure

Design plugins to fail safely:

```go
func (p *YourPlugin) processEvent(data []byte) error {
    // Validate input first
    if len(data) == 0 {
        return fmt.Errorf("empty input data")
    }
    
    if len(data) > maxInputSize {
        return fmt.Errorf("input data too large: %d bytes", len(data))
    }
    
    // Process only after validation
    return p.safeProcess(data)
}
```

## Input Validation

### Validate All Inputs

```go
import (
    "regexp"
    "unicode/utf8"
)

type InputValidator struct {
    maxStringLength int
    allowedChars    *regexp.Regexp
}

func NewInputValidator() *InputValidator {
    return &InputValidator{
        maxStringLength: 1024,
        allowedChars:    regexp.MustCompile(`^[a-zA-Z0-9\-_.]+$`),
    }
}

func (v *InputValidator) ValidateString(input string) error {
    if !utf8.ValidString(input) {
        return fmt.Errorf("invalid UTF-8 string")
    }
    
    if len(input) > v.maxStringLength {
        return fmt.Errorf("string too long: %d characters", len(input))
    }
    
    if !v.allowedChars.MatchString(input) {
        return fmt.Errorf("string contains invalid characters")
    }
    
    return nil
}

func (v *InputValidator) ValidateJSON(data []byte) error {
    var obj interface{}
    if err := json.Unmarshal(data, &obj); err != nil {
        return fmt.Errorf("invalid JSON: %w", err)
    }
    
    // Additional JSON structure validation
    return v.validateJSONStructure(obj)
}
```

### Sanitize Configuration

```go
func (p *YourPlugin) Initialize(ctx context.Context, config map[string]interface{}) error {
    // Validate configuration keys
    allowedKeys := map[string]bool{
        "port":    true,
        "path":    true,
        "timeout": true,
    }
    
    for key := range config {
        if !allowedKeys[key] {
            return fmt.Errorf("unknown configuration key: %s", key)
        }
    }
    
    // Validate and sanitize values
    if port, ok := config["port"]; ok {
        if err := p.validatePort(port); err != nil {
            return fmt.Errorf("invalid port configuration: %w", err)
        }
    }
    
    return nil
}

func (p *YourPlugin) validatePort(port interface{}) error {
    portInt, ok := port.(int)
    if !ok {
        return fmt.Errorf("port must be an integer")
    }
    
    if portInt < 1024 || portInt > 65535 {
        return fmt.Errorf("port must be between 1024 and 65535")
    }
    
    return nil
}
```

## Authentication and Authorization

### Implement Strong Authentication

```go
type AuthConfig struct {
    Type   string `yaml:"type"`   // "bearer", "basic", "mtls"
    Token  string `yaml:"token"`
    Cert   string `yaml:"cert"`
    Key    string `yaml:"key"`
    CACert string `yaml:"caCert"`
}

func (p *YourPlugin) setupAuthentication(config AuthConfig) error {
    switch config.Type {
    case "bearer":
        return p.setupBearerAuth(config.Token)
    case "basic":
        return p.setupBasicAuth(config)
    case "mtls":
        return p.setupMTLS(config)
    default:
        return fmt.Errorf("unsupported authentication type: %s", config.Type)
    }
}

func (p *YourPlugin) setupBearerAuth(token string) error {
    if len(token) < 32 {
        return fmt.Errorf("bearer token too short")
    }
    
    // Validate token format
    if !regexp.MustCompile(`^[A-Za-z0-9+/=]+$`).MatchString(token) {
        return fmt.Errorf("invalid token format")
    }
    
    p.authToken = token
    return nil
}
```

### Webhook Authentication Example

```go
func (w *WebhookEventSource) authenticateRequest(req *http.Request) error {
    switch w.authType {
    case "bearer":
        return w.validateBearerToken(req)
    case "signature":
        return w.validateSignature(req)
    default:
        return fmt.Errorf("no authentication configured")
    }
}

func (w *WebhookEventSource) validateBearerToken(req *http.Request) error {
    authHeader := req.Header.Get("Authorization")
    if authHeader == "" {
        return fmt.Errorf("missing authorization header")
    }
    
    const bearerPrefix = "Bearer "
    if !strings.HasPrefix(authHeader, bearerPrefix) {
        return fmt.Errorf("invalid authorization header format")
    }
    
    token := authHeader[len(bearerPrefix):]
    if subtle.ConstantTimeCompare([]byte(token), []byte(w.expectedToken)) != 1 {
        return fmt.Errorf("invalid token")
    }
    
    return nil
}

func (w *WebhookEventSource) validateSignature(req *http.Request) error {
    signature := req.Header.Get("X-Signature")
    if signature == "" {
        return fmt.Errorf("missing signature header")
    }
    
    body, err := io.ReadAll(req.Body)
    if err != nil {
        return fmt.Errorf("failed to read request body: %w", err)
    }
    
    // Restore body for further processing
    req.Body = io.NopCloser(bytes.NewReader(body))
    
    expectedSignature := w.calculateSignature(body)
    if subtle.ConstantTimeCompare([]byte(signature), []byte(expectedSignature)) != 1 {
        return fmt.Errorf("invalid signature")
    }
    
    return nil
}
```

## Network Security

### Use TLS for External Connections

```go
import (
    "crypto/tls"
    "crypto/x509"
)

func (p *YourPlugin) setupTLSConfig(certFile, keyFile, caCertFile string) (*tls.Config, error) {
    cert, err := tls.LoadX509KeyPair(certFile, keyFile)
    if err != nil {
        return nil, fmt.Errorf("failed to load certificate: %w", err)
    }
    
    config := &tls.Config{
        Certificates: []tls.Certificate{cert},
        MinVersion:   tls.VersionTLS12,
        CipherSuites: []uint16{
            tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
            tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
            tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
        },
    }
    
    if caCertFile != "" {
        caCert, err := os.ReadFile(caCertFile)
        if err != nil {
            return nil, fmt.Errorf("failed to read CA certificate: %w", err)
        }
        
        caCertPool := x509.NewCertPool()
        if !caCertPool.AppendCertsFromPEM(caCert) {
            return nil, fmt.Errorf("failed to parse CA certificate")
        }
        
        config.ClientCAs = caCertPool
        config.ClientAuth = tls.RequireAndVerifyClientCert
    }
    
    return config, nil
}
```

### Implement Rate Limiting

```go
import (
    "golang.org/x/time/rate"
    "sync"
)

type RateLimiter struct {
    limiters map[string]*rate.Limiter
    mu       sync.RWMutex
    rate     rate.Limit
    burst    int
}

func NewRateLimiter(r rate.Limit, b int) *RateLimiter {
    return &RateLimiter{
        limiters: make(map[string]*rate.Limiter),
        rate:     r,
        burst:    b,
    }
}

func (rl *RateLimiter) Allow(key string) bool {
    rl.mu.RLock()
    limiter, exists := rl.limiters[key]
    rl.mu.RUnlock()
    
    if !exists {
        rl.mu.Lock()
        limiter, exists = rl.limiters[key]
        if !exists {
            limiter = rate.NewLimiter(rl.rate, rl.burst)
            rl.limiters[key] = limiter
        }
        rl.mu.Unlock()
    }
    
    return limiter.Allow()
}

// Use in webhook handler
func (w *WebhookEventSource) handleWebhook(rw http.ResponseWriter, req *http.Request) {
    clientIP := getClientIP(req)
    
    if !w.rateLimiter.Allow(clientIP) {
        http.Error(rw, "Rate limit exceeded", http.StatusTooManyRequests)
        return
    }
    
    // Process request
}
```

## Data Protection

### Encrypt Sensitive Data

```go
import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "encoding/base64"
)

type DataEncryption struct {
    gcm cipher.AEAD
}

func NewDataEncryption(key []byte) (*DataEncryption, error) {
    if len(key) != 32 {
        return nil, fmt.Errorf("key must be 32 bytes")
    }
    
    block, err := aes.NewCipher(key)
    if err != nil {
        return nil, fmt.Errorf("failed to create cipher: %w", err)
    }
    
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, fmt.Errorf("failed to create GCM: %w", err)
    }
    
    return &DataEncryption{gcm: gcm}, nil
}

func (de *DataEncryption) Encrypt(plaintext []byte) (string, error) {
    nonce := make([]byte, de.gcm.NonceSize())
    if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
        return "", fmt.Errorf("failed to generate nonce: %w", err)
    }
    
    ciphertext := de.gcm.Seal(nonce, nonce, plaintext, nil)
    return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (de *DataEncryption) Decrypt(ciphertext string) ([]byte, error) {
    data, err := base64.StdEncoding.DecodeString(ciphertext)
    if err != nil {
        return nil, fmt.Errorf("failed to decode ciphertext: %w", err)
    }
    
    nonceSize := de.gcm.NonceSize()
    if len(data) < nonceSize {
        return nil, fmt.Errorf("ciphertext too short")
    }
    
    nonce, ciphertext := data[:nonceSize], data[nonceSize:]
    plaintext, err := de.gcm.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to decrypt: %w", err)
    }
    
    return plaintext, nil
}
```

### Secure Data Handling

```go
func (p *YourPlugin) processSecureData(data []byte) error {
    // Create a copy to avoid modifying original
    secureData := make([]byte, len(data))
    copy(secureData, data)
    
    defer func() {
        // Clear sensitive data from memory
        for i := range secureData {
            secureData[i] = 0
        }
    }()
    
    // Process the secure data
    return p.handleData(secureData)
}
```

## Resource Management

### Prevent Resource Exhaustion

```go
import (
    "context"
    "time"
)

type ResourceLimits struct {
    MaxConnections    int
    MaxMemoryMB      int
    MaxGoroutines    int
    ConnectionTimeout time.Duration
}

type ResourceManager struct {
    limits      ResourceLimits
    connections int32
    goroutines  int32
    mu          sync.Mutex
}

func (rm *ResourceManager) AcquireConnection(ctx context.Context) error {
    if atomic.LoadInt32(&rm.connections) >= int32(rm.limits.MaxConnections) {
        return fmt.Errorf("connection limit exceeded")
    }
    
    atomic.AddInt32(&rm.connections, 1)
    
    // Set timeout
    ctx, cancel := context.WithTimeout(ctx, rm.limits.ConnectionTimeout)
    defer cancel()
    
    return nil
}

func (rm *ResourceManager) ReleaseConnection() {
    atomic.AddInt32(&rm.connections, -1)
}

func (rm *ResourceManager) StartGoroutine(fn func()) error {
    if atomic.LoadInt32(&rm.goroutines) >= int32(rm.limits.MaxGoroutines) {
        return fmt.Errorf("goroutine limit exceeded")
    }
    
    atomic.AddInt32(&rm.goroutines, 1)
    
    go func() {
        defer atomic.AddInt32(&rm.goroutines, -1)
        fn()
    }()
    
    return nil
}
```

## Secrets Management

### Use Kubernetes Secrets

```yaml
# Create secret for plugin
apiVersion: v1
kind: Secret
metadata:
  name: webhook-plugin-secret
  namespace: kagent
type: Opaque
data:
  token: <base64-encoded-token>
  cert: <base64-encoded-cert>
  key: <base64-encoded-key>
```

```go
func (p *YourPlugin) loadSecrets() error {
    // Load from environment variables (populated by Kubernetes)
    token := os.Getenv("PLUGIN_TOKEN")
    if token == "" {
        return fmt.Errorf("PLUGIN_TOKEN environment variable not set")
    }
    
    // Validate secret format
    if len(token) < 32 {
        return fmt.Errorf("token too short")
    }
    
    p.authToken = token
    return nil
}
```

### Secure Secret Handling

```go
type SecretManager struct {
    secrets map[string][]byte
    mu      sync.RWMutex
}

func (sm *SecretManager) StoreSecret(key string, value []byte) {
    sm.mu.Lock()
    defer sm.mu.Unlock()
    
    // Store encrypted or use secure memory if available
    sm.secrets[key] = value
}

func (sm *SecretManager) GetSecret(key string) ([]byte, bool) {
    sm.mu.RLock()
    defer sm.mu.RUnlock()
    
    value, exists := sm.secrets[key]
    if !exists {
        return nil, false
    }
    
    // Return a copy to prevent modification
    result := make([]byte, len(value))
    copy(result, value)
    return result, true
}

func (sm *SecretManager) ClearSecrets() {
    sm.mu.Lock()
    defer sm.mu.Unlock()
    
    // Securely clear all secrets
    for key, value := range sm.secrets {
        for i := range value {
            value[i] = 0
        }
        delete(sm.secrets, key)
    }
}
```

## Audit and Logging

### Security Event Logging

```go
type SecurityLogger struct {
    logger logr.Logger
}

func (sl *SecurityLogger) LogAuthSuccess(source, user string) {
    sl.logger.Info("Authentication successful",
        "event", "auth_success",
        "source", source,
        "user", user,
        "timestamp", time.Now().UTC(),
    )
}

func (sl *SecurityLogger) LogAuthFailure(source, user, reason string) {
    sl.logger.Error(nil, "Authentication failed",
        "event", "auth_failure",
        "source", source,
        "user", user,
        "reason", reason,
        "timestamp", time.Now().UTC(),
    )
}

func (sl *SecurityLogger) LogSecurityEvent(eventType, description string, metadata map[string]interface{}) {
    fields := []interface{}{
        "event", eventType,
        "description", description,
        "timestamp", time.Now().UTC(),
    }
    
    for key, value := range metadata {
        fields = append(fields, key, value)
    }
    
    sl.logger.Info("Security event", fields...)
}
```

### Audit Trail

```go
type AuditEvent struct {
    Timestamp   time.Time              `json:"timestamp"`
    EventType   string                 `json:"eventType"`
    Source      string                 `json:"source"`
    User        string                 `json:"user,omitempty"`
    Resource    string                 `json:"resource,omitempty"`
    Action      string                 `json:"action"`
    Result      string                 `json:"result"`
    Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

func (p *YourPlugin) auditEvent(eventType, action, result string, metadata map[string]interface{}) {
    event := AuditEvent{
        Timestamp: time.Now().UTC(),
        EventType: eventType,
        Source:    p.Name(),
        Action:    action,
        Result:    result,
        Metadata:  metadata,
    }
    
    // Log to audit system
    p.auditLogger.LogAuditEvent(event)
}
```

## Deployment Security

### Secure Container Configuration

```yaml
# Deployment with security context
apiVersion: apps/v1
kind: Deployment
metadata:
  name: khook
  namespace: kagent
spec:
  template:
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 65534
        fsGroup: 65534
      containers:
      - name: manager
        image: khook:latest
        securityContext:
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          runAsNonRoot: true
          runAsUser: 65534
          capabilities:
            drop:
            - ALL
        resources:
          limits:
            memory: "512Mi"
            cpu: "500m"
          requests:
            memory: "256Mi"
            cpu: "250m"
        volumeMounts:
        - name: tmp
          mountPath: /tmp
        - name: cache
          mountPath: /cache
      volumes:
      - name: tmp
        emptyDir: {}
      - name: cache
        emptyDir: {}
```

### Network Policies

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: khook-network-policy
  namespace: kagent
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: khook
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: kagent
    ports:
    - protocol: TCP
      port: 8080  # Metrics
    - protocol: TCP
      port: 8081  # Health checks
  egress:
  - to: []  # Kubernetes API
    ports:
    - protocol: TCP
      port: 443
  - to: []  # Kagent API
    ports:
    - protocol: TCP
      port: 80
    - protocol: TCP
      port: 443
```

## Security Checklist

Before deploying a plugin, ensure:

- [ ] All inputs are validated and sanitized
- [ ] Authentication is implemented for external connections
- [ ] TLS is used for network communications
- [ ] Secrets are managed securely
- [ ] Resource limits are enforced
- [ ] Security events are logged
- [ ] Container runs as non-root user
- [ ] Network policies restrict traffic
- [ ] RBAC follows least privilege principle
- [ ] Error messages don't leak sensitive information
- [ ] Dependencies are regularly updated
- [ ] Security tests are included in CI/CD

## Security Testing

### Automated Security Tests

```go
func TestSecurityValidation(t *testing.T) {
    plugin := NewYourPlugin()
    
    // Test input validation
    t.Run("RejectMaliciousInput", func(t *testing.T) {
        maliciousInputs := []string{
            "<script>alert('xss')</script>",
            "'; DROP TABLE users; --",
            strings.Repeat("A", 10000),
        }
        
        for _, input := range maliciousInputs {
            err := plugin.ProcessInput(input)
            assert.Error(t, err, "Should reject malicious input: %s", input)
        }
    })
    
    // Test authentication
    t.Run("RequireAuthentication", func(t *testing.T) {
        req := httptest.NewRequest("POST", "/webhook", nil)
        rr := httptest.NewRecorder()
        
        plugin.HandleWebhook(rr, req)
        assert.Equal(t, http.StatusUnauthorized, rr.Code)
    })
}
```

Regular security assessments should include:
- Static code analysis
- Dependency vulnerability scanning
- Penetration testing
- Security code reviews
