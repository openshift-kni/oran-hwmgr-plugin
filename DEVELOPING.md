<!--
SPDX-FileCopyrightText: Red Hat

SPDX-License-Identifier: Apache-2.0
-->

# Typed Errors

The errors defined in `typed_errors.go` provide new types for a more granular error management so the business logic can address specific errors if they are defined using those types.

## Design

The error types defined embed a common error structure, `GenericError` type which :

1. Implements the error interface, `Error()`
2. Implements the unwrap method, `Unwrap()`, allowing to preserve and recover the original error. The new typed errors can therefore be part of an error chain.

The specific error type can be checked using the corresponding `Is()` function which uses `errors.As()` to identify the error type in an error chain.

## Extension

New errors can be created just implementing:

1. A new structure for the error, embedding `GenericError`, ie: `TokenError`
2. A builder function (New) to return the previous type, through an `error` interface, ie: `NewTokenError`
3. An `Is` function to check if the corresponding type is present in an error/error chain, ie: `IsTokenError`

## Unit test

These errors and combinations of them exemplifying usages are tested in `typed_errors_test.go`