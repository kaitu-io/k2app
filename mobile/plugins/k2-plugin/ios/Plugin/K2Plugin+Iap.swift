import Foundation
import Capacitor
import StoreKit

/// StoreKit 2 IAP bridge methods. See StoreKitManager for the trust/finish model.
///
/// JS contract (camelCase, per cross-layer Go→JS convention):
///   iapGetProducts({ productIds: string[] }) -> { products: IapProduct[] }
///   iapPurchase({ productId, accountToken }) -> { result, transactionId?, ... }
///   iapRestore() -> { transactions: [{ transactionId, productId }] }
///   iapFinishTransaction({ transactionId }) -> void
///   event "iapTransactionUpdate" -> { transactionId, productId }
extension K2Plugin {

    /// Begin the Transaction.updates listener. Called once from load().
    /// Forwards background renewals / interrupted purchases / Ask-to-Buy
    /// approvals to JS as the "iapTransactionUpdate" event.
    @objc func startIapListenerIfAvailable() {
        guard #available(iOS 15.0, *) else { return }
        Task {
            await StoreKitManager.shared.startListening { [weak self] transactionId, productID in
                self?.notifyListeners("iapTransactionUpdate", data: [
                    "transactionId": String(transactionId),
                    "productId": productID,
                ])
            }
        }
    }

    @objc func iapGetProducts(_ call: CAPPluginCall) {
        guard let ids = call.getArray("productIds", String.self), !ids.isEmpty else {
            call.reject("Missing productIds")
            return
        }
        guard #available(iOS 15.0, *) else {
            call.reject("StoreKit 2 requires iOS 15")
            return
        }
        Task {
            do {
                let products = try await StoreKitManager.shared.products(for: ids)
                let mapped = products.map { product -> [String: Any] in
                    var periodUnit: String?
                    var periodValue: Int?
                    if let period = product.subscription?.subscriptionPeriod {
                        // Product.SubscriptionPeriod.Unit is NOT raw-representable —
                        // map it to a stable code here (the StoreKit boundary), then
                        // let the pure helper name it.
                        let unitCode: Int
                        switch period.unit {
                        case .day: unitCode = 0
                        case .week: unitCode = 1
                        case .month: unitCode = 2
                        case .year: unitCode = 3
                        @unknown default: unitCode = -1
                        }
                        periodUnit = IapHelpers.periodUnitString(rawValue: unitCode)
                        periodValue = period.value
                    }
                    return IapHelpers.productDict(
                        id: product.id,
                        displayName: product.displayName,
                        description: product.description,
                        displayPrice: product.displayPrice,
                        price: NSDecimalNumber(decimal: product.price).doubleValue,
                        periodUnit: periodUnit,
                        periodValue: periodValue
                    )
                }
                call.resolve(["products": mapped])
            } catch {
                call.reject("getProducts failed: \(error.localizedDescription)")
            }
        }
    }

    @objc func iapPurchase(_ call: CAPPluginCall) {
        guard let productId = call.getString("productId") else {
            call.reject("Missing productId")
            return
        }
        guard let tokenStr = call.getString("accountToken"), let accountToken = UUID(uuidString: tokenStr) else {
            // accountToken must be an RFC 4122 UUID (Center derives it via uuidv5
            // from the user UUID — the raw Kaitu user id is NOT a UUID).
            call.reject("Missing or malformed accountToken (expected UUID)")
            return
        }
        guard #available(iOS 15.0, *) else {
            call.reject("StoreKit 2 requires iOS 15")
            return
        }
        Task {
            do {
                let products = try await StoreKitManager.shared.products(for: [productId])
                guard let product = products.first else {
                    call.reject("Product not found: \(productId)")
                    return
                }
                let outcome = try await StoreKitManager.shared.purchase(product, accountToken: accountToken)
                switch outcome {
                case .success(let transaction):
                    call.resolve([
                        "result": "success",
                        "transactionId": String(transaction.id),
                        "originalTransactionId": String(transaction.originalID),
                        "productId": transaction.productID,
                    ])
                case .cancelled:
                    call.resolve(["result": "cancelled"])
                case .pending:
                    call.resolve(["result": "pending"])
                }
            } catch {
                call.reject("purchase failed: \(error.localizedDescription)")
            }
        }
    }

    @objc func iapRestore(_ call: CAPPluginCall) {
        guard #available(iOS 15.0, *) else {
            call.reject("StoreKit 2 requires iOS 15")
            return
        }
        Task {
            let transactions = await StoreKitManager.shared.currentEntitlements()
            let mapped = transactions.map { transaction in
                return [
                    "transactionId": String(transaction.id),
                    "productId": transaction.productID,
                ]
            }
            call.resolve(["transactions": mapped])
        }
    }

    @objc func iapFinishTransaction(_ call: CAPPluginCall) {
        guard let idStr = call.getString("transactionId"), let id = UInt64(idStr) else {
            call.reject("Missing or malformed transactionId")
            return
        }
        guard #available(iOS 15.0, *) else {
            call.reject("StoreKit 2 requires iOS 15")
            return
        }
        Task {
            await StoreKitManager.shared.finish(transactionId: id)
            call.resolve()
        }
    }
}
