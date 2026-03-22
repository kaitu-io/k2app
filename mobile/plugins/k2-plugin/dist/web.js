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
    async downloadNativeUpdate() {
        throw this.unavailable('K2Plugin is not available on web');
    }
    async installNativeUpdate(_options) {
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
}
