import XCTest

/// Tests for pure IAP boundary-shaping helpers (IapHelpers.swift).
/// Run on simulator — no StoreKit, sandbox, or real device required.
class IapHelpersTests: XCTestCase {

    // MARK: - periodUnitString

    func testPeriodUnitString_allKnownUnits() {
        XCTAssertEqual(IapHelpers.periodUnitString(rawValue: 0), "day")
        XCTAssertEqual(IapHelpers.periodUnitString(rawValue: 1), "week")
        XCTAssertEqual(IapHelpers.periodUnitString(rawValue: 2), "month")
        XCTAssertEqual(IapHelpers.periodUnitString(rawValue: 3), "year")
    }

    func testPeriodUnitString_unknownUnit() {
        XCTAssertEqual(IapHelpers.periodUnitString(rawValue: 99), "unknown")
        XCTAssertEqual(IapHelpers.periodUnitString(rawValue: -1), "unknown")
    }

    // MARK: - productDict

    func testProductDict_subscriptionHasPeriod() {
        let dict = IapHelpers.productDict(
            id: "io.kaitu.sub.family.1y",
            displayName: "Family Yearly",
            description: "Kaitu Family — billed yearly",
            displayPrice: "US$39.99",
            price: 39.99,
            periodUnit: "year",
            periodValue: 1
        )
        XCTAssertEqual(dict["id"] as? String, "io.kaitu.sub.family.1y")
        XCTAssertEqual(dict["displayName"] as? String, "Family Yearly")
        XCTAssertEqual(dict["displayPrice"] as? String, "US$39.99")
        XCTAssertEqual(dict["price"] as? Double, 39.99)
        XCTAssertEqual(dict["periodUnit"] as? String, "year")
        XCTAssertEqual(dict["periodValue"] as? Int, 1)
    }

    func testProductDict_omitsPeriodWhenAbsent() {
        // A non-subscription product (no period) must not carry period keys —
        // the webapp uses their presence to decide subscription vs one-time UI.
        let dict = IapHelpers.productDict(
            id: "io.kaitu.onetime",
            displayName: "One Time",
            description: "",
            displayPrice: "US$1.99",
            price: 1.99,
            periodUnit: nil,
            periodValue: nil
        )
        XCTAssertNil(dict["periodUnit"])
        XCTAssertNil(dict["periodValue"])
        XCTAssertEqual(dict["price"] as? Double, 1.99)
    }

    func testProductDict_partialPeriodIsOmitted() {
        // Defensive: unit without value (or vice versa) is incoherent → omit both.
        let dict = IapHelpers.productDict(
            id: "x", displayName: "x", description: "", displayPrice: "US$1",
            price: 1.0, periodUnit: "month", periodValue: nil
        )
        XCTAssertNil(dict["periodUnit"])
        XCTAssertNil(dict["periodValue"])
    }
}
