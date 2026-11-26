package r18

import "crypto/tls"

// insecureTLS matches the javdb client's behavior: --insecure disables
// certificate verification for MITM proxies.
var insecureTLS = &tls.Config{InsecureSkipVerify: true}
