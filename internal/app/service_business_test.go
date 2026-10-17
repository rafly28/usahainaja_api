package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestBusinessLogoAcceptsURLAndDataURI(t *testing.T) {
	owner := BusinessContext{Role: "OWNER"}
	service := NewService(&repositoryStub{}, time.Hour, bcrypt.MinCost)

	for _, valid := range []string{
		"https://cdn.contoh.com/logo.png",
		"http://cdn.contoh.com/logo.png",
		"data:image/png;base64,iVBORw0KGgo=",
	} {
		logo := valid
		if _, err := service.UpdateBusinessConfiguration(context.Background(), Session{UserID: "owner-id"}, owner,
			UpdateBusinessConfigurationInput{BusinessType: "RETAIL", EnabledModules: []string{"INVENTORY"}, LogoURL: &logo}); err != nil {
			t.Fatalf("logo %q seharusnya diterima: %v", valid, err)
		}
	}
}

func TestBusinessLogoRejectsUnsafeValues(t *testing.T) {
	owner := BusinessContext{Role: "OWNER"}
	service := NewService(&repositoryStub{}, time.Hour, bcrypt.MinCost)

	for _, invalid := range []string{
		"javascript:alert(1)",
		"ftp://contoh.com/logo.png",
		"data:text/html;base64,PGgxPmhpPC9oMT4=",
		"data:image/png,notbase64",
	} {
		logo := invalid
		_, err := service.UpdateBusinessConfiguration(context.Background(), Session{UserID: "owner-id"}, owner,
			UpdateBusinessConfigurationInput{BusinessType: "RETAIL", EnabledModules: []string{"INVENTORY"}, LogoURL: &logo})
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.Code != "VALIDATION_ERROR" {
			t.Fatalf("logo %q seharusnya ditolak, dapat %#v", invalid, err)
		}
	}
}

func TestBusinessLogoEmptyStringClearsLogo(t *testing.T) {
	owner := BusinessContext{Role: "OWNER"}
	service := NewService(&repositoryStub{}, time.Hour, bcrypt.MinCost)

	empty := "  "
	business, err := service.UpdateBusinessConfiguration(context.Background(), Session{UserID: "owner-id"}, owner,
		UpdateBusinessConfigurationInput{BusinessType: "RETAIL", EnabledModules: []string{"INVENTORY"}, LogoURL: &empty})
	if err != nil {
		t.Fatalf("string kosong berarti menghapus logo, bukan error: %v", err)
	}
	if business.LogoURL != "" {
		t.Fatalf("logo harus kosong, dapat %q", business.LogoURL)
	}
}

func TestBusinessLogoOmittedKeepsExistingLogo(t *testing.T) {
	service := NewService(&repositoryStub{}, time.Hour, bcrypt.MinCost)
	owner := BusinessContext{Role: "OWNER", Business: Business{LogoURL: "https://cdn.contoh.com/logo.png"}}

	business, err := service.UpdateBusinessConfiguration(context.Background(), Session{UserID: "owner-id"}, owner,
		UpdateBusinessConfigurationInput{BusinessType: "RETAIL", EnabledModules: []string{"INVENTORY"}})
	if err != nil {
		t.Fatalf("kegagalan tak terduga: %v", err)
	}
	if business.LogoURL != owner.LogoURL {
		t.Fatalf("logo lama harus dipertahankan, dapat %q", business.LogoURL)
	}
}
