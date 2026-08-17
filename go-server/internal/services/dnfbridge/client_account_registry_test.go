package dnfbridge

import "testing"

func TestGameSessionAccountOverridesInstanceFallback(t *testing.T) {
	service := &Service{}
	service.options.accountID = "fallback-account"
	session := &gameSession{accountID: "session-account"}
	if got := service.accountIDForSession(session); got != "session-account" {
		t.Fatalf("session account=%q, want session-account", got)
	}
	if got := service.accountIDForSession(nil); got != "fallback-account" {
		t.Fatalf("fallback account=%q, want fallback-account", got)
	}
}

func TestClientAccountRegistryBindsDistinctPIDs(t *testing.T) {
	service := &Service{clientAccounts: newClientAccountRegistry()}
	if err := service.RegisterClientAccount(101, "account-a"); err != nil {
		t.Fatal(err)
	}
	if err := service.RegisterClientAccount(202, "account-b"); err != nil {
		t.Fatal(err)
	}
	if got, ok := service.registeredClientAccount(101); !ok || got != "account-a" {
		t.Fatalf("PID 101 account=%q found=%t", got, ok)
	}
	if got, ok := service.registeredClientAccount(202); !ok || got != "account-b" {
		t.Fatalf("PID 202 account=%q found=%t", got, ok)
	}
}

func TestClientAccountRegistryOwnsSpendTimeDescriptorPerPIDAndRegistrationResetsIt(t *testing.T) {
	service := &Service{clientAccounts: newClientAccountRegistry()}
	if err := service.RegisterClientAccount(101, "account-a"); err != nil {
		t.Fatal(err)
	}
	if err := service.RegisterClientAccount(101, "account-a"); err != nil {
		t.Fatal(err)
	}
	first := &gameSession{clientPID: 101}
	second := &gameSession{clientPID: 101}
	if send, ready := service.beginCurrentSpendTimeEventInfo(first); !send || !ready {
		t.Fatalf("first reservation send=%t ready=%t", send, ready)
	}
	if send, ready := service.beginCurrentSpendTimeEventInfo(second); send || ready {
		t.Fatalf("concurrent reservation send=%t ready=%t", send, ready)
	}
	service.finishCurrentSpendTimeEventInfo(first, true)
	if send, ready := service.beginCurrentSpendTimeEventInfo(second); send || !ready || !second.spendTime.eventInfoSent {
		t.Fatalf("same PID after send=%t ready=%t session_bit=%t", send, ready, second.spendTime.eventInfoSent)
	}
	if err := service.RegisterClientAccount(101, "account-a"); err != nil {
		t.Fatal(err)
	}
	third := &gameSession{clientPID: 101}
	if send, ready := service.beginCurrentSpendTimeEventInfo(third); !send || !ready {
		t.Fatalf("new process lifecycle did not reset descriptor send=%t ready=%t", send, ready)
	}
	service.finishCurrentSpendTimeEventInfo(third, false)
	if send, ready := service.beginCurrentSpendTimeEventInfo(third); !send || !ready {
		t.Fatalf("failed write did not release descriptor reservation send=%t ready=%t", send, ready)
	}
}

func TestClientAccountRegistryPIDZeroUsesProgressOnly(t *testing.T) {
	service := &Service{clientAccounts: newClientAccountRegistry()}
	session := &gameSession{}
	if send, ready := service.beginCurrentSpendTimeEventInfo(session); send || !ready {
		t.Fatalf("PID zero descriptor send=%t ready=%t", send, ready)
	}
	if !session.spendTime.eventInfoSent {
		t.Fatal("PID zero progress-only gate was not closed")
	}
}

func TestClientAccountRegistryRecreatedForRunningPIDUsesProgressOnly(t *testing.T) {
	service := &Service{clientAccounts: newClientAccountRegistry()}
	session := &gameSession{clientPID: 101}
	if send, ready := service.beginCurrentSpendTimeEventInfo(session); send || !ready {
		t.Fatalf("unknown running PID descriptor send=%t ready=%t", send, ready)
	}
	if !session.spendTime.eventInfoSent {
		t.Fatal("progress-only recovery did not close the session descriptor gate")
	}
}
