import { WebPlugin } from '@capacitor/core';
import type { K2PluginInterface, WebUpdateInfo, NativeUpdateInfo } from './definitions';
export declare class K2PluginWeb extends WebPlugin implements K2PluginInterface {
    checkReady(): Promise<{
        ready: boolean;
        version?: string;
        reason?: string;
    }>;
    getVersion(): Promise<{
        version: string;
        go: string;
        os: string;
        arch: string;
    }>;
    getStatus(): Promise<{
        state: string;
        connectedAt?: string;
        error?: string;
    }>;
    getConfig(): Promise<{
        config?: string;
    }>;
    connect(_options: {
        config: string;
    }): Promise<void>;
    disconnect(): Promise<void>;
    checkWebUpdate(): Promise<WebUpdateInfo>;
    checkNativeUpdate(): Promise<NativeUpdateInfo>;
    applyWebUpdate(): Promise<void>;
    downloadNativeUpdate(): Promise<{
        path: string;
    }>;
    installNativeUpdate(_options: {
        path: string;
    }): Promise<void>;
    appendLogs(_options: {
        entries: Array<{
            level: string;
            message: string;
            timestamp: number;
        }>;
    }): Promise<void>;
    uploadLogs(_options: {
        email?: string;
        reason: string;
        feedbackId?: string;
        platform?: string;
        version?: string;
    }): Promise<{
        success: boolean;
        error?: string;
        s3Keys?: Array<{
            name: string;
            s3Key: string;
        }>;
    }>;
    setLogLevel(_options: {
        level: string;
    }): Promise<void>;
    setDevEnabled(_options: {
        enabled: boolean;
    }): Promise<void>;
    debugDump(): Promise<Record<string, unknown>>;
    getUpdateChannel(): Promise<{
        channel: string;
    }>;
    setUpdateChannel(_options: {
        channel: string;
    }): Promise<{
        channel: string;
    }>;
}
