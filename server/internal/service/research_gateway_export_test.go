package service

import "net/http"

// Keep endpoint substitution inside the Go test binary. Production retains the
// fixed public DNS/TLS transport and redirect policy for every provider.
func(s *Service)UseResearchTransportForTest(transport http.RoundTripper){
	s.researchGateway.client.Transport=transport
}
