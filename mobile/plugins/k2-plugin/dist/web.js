import { WebPlugin } from '@capacitor/core';
export class K2PluginWeb extends WebPlugin {
    async checkReady() {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async getVersion() {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async getStatus() {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async getConfig() {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async connect(_options) {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async disconnect() {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async checkWebUpdate() {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async checkNativeUpdate() {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async applyWebUpdate() {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async openUrl(_options) {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async appendLogs(_options) {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async uploadLogs(_options) {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async setLogLevel(_options) {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async setDevEnabled(_options) {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async debugDump() {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async getUpdateChannel() {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async setUpdateChannel(_options) {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async storageGet(_options) {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async storageSet(_options) {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async storageRemove(_options) {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async listInstalledApps() {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async classifyApps() {
        // Fail-soft (not throw): App Bypass on web degrades to all-proxy badges.
        return { classifications: [] };
    }
    async relayFetch() {
        // Web has no gomobile core — report relay unsupported (code:-1) so the
        // webapp transport learns to skip relay and use the direct fallback.
        return { response: JSON.stringify({ code: -1, message: 'relay unsupported on web' }) };
    }
    async relayAddNodes() {
        // No relay on web → node feed is a silent no-op (success envelope so callers
        // don't log an error for a platform that legitimately has no RelayManager).
        return { response: JSON.stringify({ code: 0, message: 'ok', data: { added: 0, total: 0 } }) };
    }
    async iapGetProducts() {
        throw this.unavailable('IAP is not available on web');
    }
    async iapPurchase() {
        throw this.unavailable('IAP is not available on web');
    }
    async iapRestore() {
        throw this.unavailable('IAP is not available on web');
    }
    async iapFinishTransaction() {
        throw this.unavailable('IAP is not available on web');
    }
}
