package svc

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/andrewthowell/budgit/budgit"
	"github.com/andrewthowell/budgit/budgit/db"
	"github.com/andrewthowell/budgit/budgit/db/dbconvert"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/exp/maps"
)

type TransactionDB interface {
	InsertTransactions(ctx context.Context, queryer db.Queryer, transactions ...*db.Transaction) ([]string, error)
}

func (s Service) CreateTransactions(ctx context.Context, transactions ...*budgit.Transaction) ([]*budgit.Transaction, error) {
	var createdTransactions []*budgit.Transaction
	err := s.inTx(ctx, func(conn Conn) error {
		if err := s.validateTransactions(ctx, conn, transactions...); err != nil {
			return err
		}

		transactions, err := appendMirrorTransactions(transactions...)
		if err != nil {
			return err
		}

		now, err := s.db.Now(ctx, conn)
		if err != nil {
			return err
		}

		dbTransactions := dbconvert.FromTransactions(transactions...)
		for _, dbTransaction := range dbTransactions {
			dbTransaction.ValidFromTimestamp = now
			dbTransaction.ValidToTimestamp = pgtype.Timestamptz{InfinityModifier: pgtype.Infinity, Valid: true}
		}

		// TODO: check for transactions not being inserted
		if _, err := s.db.InsertTransactions(ctx, conn, dbTransactions...); err != nil {
			return err
		}

		balanceChangeByAccountID := balanceChangesByAccount(transactions)
		dbAccounts, err := s.db.SelectAccountsByID(ctx, conn, maps.Keys(balanceChangeByAccountID)...)
		if err != nil {
			return fmt.Errorf("updating affected account balances: %w", err)
		}
		accounts := dbconvert.ToAccounts(maps.Values(dbAccounts)...)

		for _, account := range accounts {
			account.Balance.Add(balanceChangeByAccountID[account.ID])
		}
		if _, err := s.db.InsertAccounts(ctx, s.conn, dbconvert.FromAccounts(accounts...)...); err != nil {
			return fmt.Errorf("updating affected account balances: %w", err)
		}

		// TODO: Update Category Balances

		createdTransactions = transactions
		return nil
	}, pgx.TxOptions{AccessMode: pgx.ReadWrite})
	if err != nil {
		return nil, fmt.Errorf("creating transactions: %w", err)
	}
	return createdTransactions, nil
}

type MissingAccountsError struct {
	AccountIDs []string
}

func (e MissingAccountsError) Error() string {
	return fmt.Sprintf("transactions reference Accounts that do not exist: %+v", e.AccountIDs)
}

type MissingPayeesError struct {
	PayeeIDs []string
}

func (e MissingPayeesError) Error() string {
	return fmt.Sprintf("transactions reference Payees that do not exist: %+v", e.PayeeIDs)
}

func (s Service) validateTransactions(ctx context.Context, conn Conn, transactions ...*budgit.Transaction) error {
	errs := []error{}

	accountIDs := make([]string, 0, len(transactions))
	payeeIDs := make([]string, 0, len(transactions))
	for _, transaction := range transactions {
		accountIDs = append(accountIDs, transaction.AccountID)
		if transaction.IsPayeeInternal {
			accountIDs = append(accountIDs, transaction.PayeeID)
		} else {
			payeeIDs = append(payeeIDs, transaction.PayeeID)
		}
	}

	uniqueAccountIDs := deduplicate(accountIDs)
	foundAccounts, err := s.db.SelectAccountsByID(ctx, conn, uniqueAccountIDs...)
	if err != nil {
		return fmt.Errorf("validating transactions: %w", err)
	}
	if len(foundAccounts) < len(uniqueAccountIDs) {
		missingIDs := symmetricDifference(uniqueAccountIDs, maps.Keys(foundAccounts))
		errs = append(errs, MissingAccountsError{AccountIDs: missingIDs})
	}

	uniquePayeeIDs := deduplicate(payeeIDs)
	foundPayees, err := s.db.SelectPayeesByID(ctx, conn, uniquePayeeIDs...)
	if err != nil {
		return fmt.Errorf("validating transactions: %w", err)
	}
	if len(foundPayees) < len(uniquePayeeIDs) {
		missingIDs := symmetricDifference(uniquePayeeIDs, maps.Keys(foundPayees))
		errs = append(errs, MissingPayeesError{PayeeIDs: missingIDs})
	}

	if len(errs) != 0 {
		return errors.Join(errs...)
	}
	return nil
}

func appendMirrorTransactions(transactions ...*budgit.Transaction) ([]*budgit.Transaction, error) {
	mirrorTransactions := make([]*budgit.Transaction, 0, len(transactions))
	for _, transaction := range transactions {
		if transaction.IsPayeeInternal {
			mirrorTransactions = append(mirrorTransactions, transaction.Mirror(uuid.New().String()))
		}
	}
	return slices.Concat(transactions, mirrorTransactions), nil
}

func balanceChangesByAccount(transactions []*budgit.Transaction) map[string]budgit.Balance {
	balanceChangeByAccountID := make(map[string]budgit.Balance, len(transactions))
	for _, transaction := range transactions {
		balanceChangeByAccountID[transaction.AccountID] = balanceChangeByAccountID[transaction.AccountID].AddAmount(transaction.Amount, transaction.Cleared)
	}
	return balanceChangeByAccountID
}
