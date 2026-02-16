module("luci.controller.k2", package.seeall)

function index()
    entry({"admin", "services", "k2"}, template("k2"), _("K2 VPN"), 90)
end
