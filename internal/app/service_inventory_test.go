package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestOpeningStockRejectsDuplicateProductInBatch(t *testing.T) {
	service := NewService(&repositoryStub{}, time.Hour, bcrypt.MinCost)
	_, err := service.CreateOpeningStock(context.Background(), Session{UserID: "user-id"}, "business-id", OpeningStockInput{
		Items: []OpeningStockItemInput{
			{ProductCode: "PRD-000001", Quantity: "2"},
			{ProductCode: "prd-000001", Quantity: "3"},
		},
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "VALIDATION_ERROR" {
		t.Fatalf("expected validation error for duplicated product, got %#v", err)
	}
}

func TestStockAdjustmentRequiresValidReasonAndDirection(t *testing.T) {
	service := NewService(&repositoryStub{}, time.Hour, bcrypt.MinCost)
	_, err := service.CreateStockAdjustment(context.Background(), Session{UserID: "user-id"}, "business-id", NewStockAdjustment{
		LocationCode: "LOC-DEFAULT",
		Reason:       "UNKNOWN_REASON",
		Items:        []NewStockAdjustmentItem{{ProductCode: "PRD-000001", Quantity: "1", Direction: "SIDEWAYS"}},
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "VALIDATION_ERROR" {
		t.Fatalf("expected validation error, got %#v", err)
	}
}

func TestStockMovementFilterRejectsUnknownMovementType(t *testing.T) {
	service := NewService(&repositoryStub{}, time.Hour, bcrypt.MinCost)
	_, _, err := service.ListStockMovements(context.Background(), "business-id", StockMovementFilter{MovementType: "CORRECTION"})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "VALIDATION_ERROR" {
		t.Fatalf("expected validation error for unknown movement type, got %#v", err)
	}
}

func TestStockMovementFilterAcceptsContractDates(t *testing.T) {
	repo := &repositoryStub{}
	service := NewService(repo, time.Hour, bcrypt.MinCost)
	if _, _, err := service.ListStockMovements(context.Background(), "business-id", StockMovementFilter{
		DateFrom: "2026-08-01", DateTo: "2026-08-31",
	}); err != nil {
		t.Fatalf("valid date filter rejected: %v", err)
	}
	if _, _, err := service.ListStockMovements(context.Background(), "business-id", StockMovementFilter{
		DateFrom: "01-08-2026",
	}); err == nil {
		t.Fatal("expected invalid date format to be rejected")
	}
}

func TestStockOpnameRejectsDuplicateProduct(t *testing.T) {
	service := NewService(&repositoryStub{}, time.Hour, bcrypt.MinCost)
	_, err := service.CreateStockOpname(context.Background(), Session{UserID: "user-id"}, "business-id", NewStockOpname{
		Items: []NewStockOpnameItem{
			{ProductCode: "PRD-000001", CountedQuantity: "5"},
			{ProductCode: "PRD-000001", CountedQuantity: "6"},
		},
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "VALIDATION_ERROR" {
		t.Fatalf("expected validation error, got %#v", err)
	}
}

func TestStockOpnameAllowsZeroCountedQuantity(t *testing.T) {
	service := NewService(&repositoryStub{}, time.Hour, bcrypt.MinCost)
	_, err := service.CreateStockOpname(context.Background(), Session{UserID: "user-id"}, "business-id", NewStockOpname{
		Items: []NewStockOpnameItem{{ProductCode: "PRD-000001", CountedQuantity: "0"}},
	})
	if err != nil {
		t.Fatalf("counted quantity 0 must be allowed: %v", err)
	}
}

func TestCompleteStockAdjustmentMapsInsufficientStock(t *testing.T) {
	repo := &repositoryStub{CompleteStockAdjustmentFunc: func(context.Context, string, string, string) error {
		return ErrInsufficientStock
	}}
	service := NewService(repo, time.Hour, bcrypt.MinCost)
	err := service.CompleteStockAdjustment(context.Background(), Session{UserID: "user-id"}, "business-id", "ADJ-000001")
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Code != "INSUFFICIENT_STOCK" {
		t.Fatalf("expected INSUFFICIENT_STOCK, got %#v", err)
	}
}
