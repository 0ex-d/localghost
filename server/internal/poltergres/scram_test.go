package poltergres

import "testing"

// TestScramRFC7677Vector drives the client against the published RFC 7677 example. With the client
// nonce and password fixed to the RFC's values, clientFinal must produce the RFC's exact proof, and
// verifyServer must accept the RFC's server signature. If SCRAM is wrong, no service role can log in,
// so this vector is the gate.
//
// RFC 7677 section 3:
//   username "user", password "pencil"
//   client nonce  rOprNGfwEbeRWgbNEkqO
//   server-first  r=rOprNGfwEbeRWgbNEkqO%hvYDpWUa2RaTCAfuxFIlj)hNlF$k0,s=W22ZaJ0SNY7soEsUEjb6gQ==,i=4096
//   client-final  c=biws,r=rOprNGfwEbeRWgbNEkqO%hvYDpWUa2RaTCAfuxFIlj)hNlF$k0,p=dHzbZapWIk4jUhN+Ute9ytag9zjfMHgsqmmiz7AndVQ=
//   server-final  v=6rriTRBi23WpRR/wtup+mMhUZUn/dB5nLTJRsjl95G4=
func TestScramRFC7677Vector(t *testing.T) {
	s := &scram{pass: "pencil", user: "user", nonce: "rOprNGfwEbeRWgbNEkqO"}

	first := s.clientFirst()
	if first != "n,,n=user,r=rOprNGfwEbeRWgbNEkqO" {
		t.Fatalf("client-first = %q", first)
	}

	serverFirst := "r=rOprNGfwEbeRWgbNEkqO%hvYDpWUa2RaTCAfuxFIlj)hNlF$k0," +
		"s=W22ZaJ0SNY7soEsUEjb6gQ==,i=4096"
	final, err := s.clientFinal(serverFirst)
	if err != nil {
		t.Fatal(err)
	}
	want := "c=biws,r=rOprNGfwEbeRWgbNEkqO%hvYDpWUa2RaTCAfuxFIlj)hNlF$k0," +
		"p=dHzbZapWIk4jUhN+Ute9ytag9zjfMHgsqmmiz7AndVQ="
	if final != want {
		t.Fatalf("client-final mismatch:\n got %q\nwant %q", final, want)
	}

	if err := s.verifyServer("v=6rriTRBi23WpRR/wtup+mMhUZUn/dB5nLTJRsjl95G4="); err != nil {
		t.Fatalf("server signature must verify: %v", err)
	}
	if err := s.verifyServer("v=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="); err == nil {
		t.Fatal("a wrong server signature must be rejected")
	}
}

// TestScramNonceExtensionCheck: the server nonce must start with the client nonce, else a MITM could
// substitute its own; clientFinal must refuse.
func TestScramNonceExtensionCheck(t *testing.T) {
	s := &scram{pass: "pencil", nonce: "clientnonce"}
	_ = s.clientFirst()
	if _, err := s.clientFinal("r=DIFFERENTnonce,s=W22ZaJ0SNY7soEsUEjb6gQ==,i=4096"); err == nil {
		t.Fatal("must reject a server nonce that does not extend the client nonce")
	}
}
