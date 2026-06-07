import Foundation

/// Pure, StoreKit-free helpers for the IAP bridge so the JS-boundary shaping
/// is unit-testable in K2Tests without a StoreKit environment.
enum IapHelpers {
    /// Map a subscription-period unit code to a stable JS string. The codes
    /// (day=0, week=1, month=2, year=3) are assigned by the caller via a switch
    /// over `Product.SubscriptionPeriod.Unit` (which is NOT raw-representable),
    /// keeping this helper free of any StoreKit dependency so it's unit-testable.
    static func periodUnitString(rawValue: Int) -> String {
        switch rawValue {
        case 0: return "day"
        case 1: return "week"
        case 2: return "month"
        case 3: return "year"
        default: return "unknown"
        }
    }

    /// Assemble the JS-facing product dictionary. Kept pure (primitives in,
    /// dictionary out) so the Go→JS camelCase contract is asserted in tests.
    static func productDict(
        id: String,
        displayName: String,
        description: String,
        displayPrice: String,
        price: Double,
        periodUnit: String?,
        periodValue: Int?
    ) -> [String: Any] {
        var dict: [String: Any] = [
            "id": id,
            "displayName": displayName,
            "description": description,
            "displayPrice": displayPrice,
            "price": price,
        ]
        if let unit = periodUnit, let value = periodValue {
            dict["periodUnit"] = unit
            dict["periodValue"] = value
        }
        return dict
    }
}
