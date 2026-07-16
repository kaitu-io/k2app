import Foundation
import StoreKit
import os.log

/// StoreKit 2 facade for auto-renewable subscriptions.
///
/// Trust model: this layer NEVER grants entitlement. It returns the
/// transaction id to the webapp, which calls Center `/api/user/apple-iap/verify`;
/// Center re-fetches via App Store Server API `GetTransaction` (authenticated TLS)
/// as the single load-bearing authority. Local StoreKit verification here is
/// advisory only — used to reject obviously-tampered payloads early.
///
/// finish() discipline: a purchased/restored transaction is retained UNFINISHED
/// until the webapp confirms Center granted entitlement, then calls
/// `iapFinishTransaction`. If the app dies in between, `Transaction.updates`
/// re-delivers it on next launch and the verify→finish loop retries. This is the
/// durability guarantee against lost purchases.
///
/// Actor isolation makes the `pending` map race-free across the purchase Task,
/// the restore Task, and the long-lived `Transaction.updates` listener Task.
@available(iOS 15.0, *)
actor StoreKitManager {
    static let shared = StoreKitManager()

    private let logger = Logger(subsystem: Bundle.main.bundleIdentifier ?? "com.allnationconnect.anc.wgios", category: "storekit")

    /// Transactions delivered to JS but not yet finished (awaiting Center grant).
    private var pending: [UInt64: StoreKit.Transaction] = [:]

    /// Long-lived listener over Transaction.updates (renewals, interrupted
    /// purchases, Ask-to-Buy approvals, cross-device).
    private var updatesTask: Task<Void, Never>?

    /// Set by the plugin in load(); forwards (transactionId, productId) to JS.
    private var updateHandler: ((UInt64, String) -> Void)?

    enum IapError: LocalizedError {
        case unverified
        case userCancelled
        case pending
        case productNotFound
        case unknown

        var errorDescription: String? {
            switch self {
            case .unverified: return "StoreKit transaction failed local verification"
            case .userCancelled: return "Purchase cancelled by user"
            case .pending: return "Purchase pending (Ask to Buy / SCA)"
            case .productNotFound: return "Product not found in App Store"
            case .unknown: return "Unknown StoreKit error"
            }
        }
    }

    /// Outcome of a purchase attempt, mapped to a JS-friendly shape by the plugin.
    enum PurchaseOutcome {
        case success(StoreKit.Transaction)
        case cancelled
        case pending
    }

    // MARK: - Listener lifecycle

    /// Begin observing Transaction.updates. Idempotent.
    func startListening(_ handler: @escaping (UInt64, String) -> Void) {
        updateHandler = handler
        guard updatesTask == nil else { return }
        updatesTask = Task { [weak self] in
            guard let self = self else { return }
            for await result in StoreKit.Transaction.updates {
                guard case .verified(let transaction) = result else {
                    await self.logUnverifiedUpdate()
                    continue
                }
                await self.retain(transaction)
                await self.emit(transaction)
            }
        }
    }

    private func logUnverifiedUpdate() {
        logger.warning("Transaction.updates delivered an unverified transaction; ignoring")
    }

    private func emit(_ transaction: StoreKit.Transaction) {
        updateHandler?(transaction.id, transaction.productID)
    }

    // MARK: - Products

    func products(for ids: [String]) async throws -> [Product] {
        return try await Product.products(for: ids)
    }

    // MARK: - Purchase

    /// Purchase `product`, binding it to the Kaitu account via appAccountToken.
    /// Retains the transaction unfinished on success.
    func purchase(_ product: Product, accountToken: UUID) async throws -> PurchaseOutcome {
        let result = try await product.purchase(options: [.appAccountToken(accountToken)])
        switch result {
        case .success(let verification):
            let transaction = try checkVerified(verification)
            retain(transaction)
            return .success(transaction)
        case .userCancelled:
            return .cancelled
        case .pending:
            return .pending
        @unknown default:
            throw IapError.unknown
        }
    }

    // MARK: - Restore

    /// Current entitlements (active subscriptions), retained unfinished so the
    /// webapp can re-verify each against Center. This is the StoreKit 2 restore
    /// path — no deprecated receipt refresh.
    func currentEntitlements() async -> [StoreKit.Transaction] {
        var out: [StoreKit.Transaction] = []
        for await result in StoreKit.Transaction.currentEntitlements {
            guard case .verified(let transaction) = result else { continue }
            retain(transaction)
            out.append(transaction)
        }
        return out
    }

    // MARK: - Finish

    /// Finish a transaction once Center has granted entitlement. Safe to call
    /// for an unknown id (no-op) — covers retries after the in-memory map was
    /// lost to an app restart (Transaction.updates will re-deliver it).
    func finish(transactionId: UInt64) async {
        if let transaction = pending[transactionId] {
            await transaction.finish()
            pending[transactionId] = nil
            return
        }
        // Map lost (app restarted). Scan unfinished and match by id.
        for await result in StoreKit.Transaction.unfinished {
            guard case .verified(let transaction) = result else { continue }
            if transaction.id == transactionId {
                await transaction.finish()
                return
            }
        }
    }

    // MARK: - Helpers

    private func retain(_ transaction: StoreKit.Transaction) {
        pending[transaction.id] = transaction
    }

    private func checkVerified<T>(_ result: VerificationResult<T>) throws -> T {
        switch result {
        case .unverified:
            throw IapError.unverified
        case .verified(let safe):
            return safe
        }
    }
}
