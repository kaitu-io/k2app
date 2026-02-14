#import <Foundation/Foundation.h>
#import <Capacitor/Capacitor.h>

CAP_PLUGIN(K2Plugin, "K2Plugin",
    CAP_PLUGIN_METHOD(checkReady, CAPPluginReturnPromise);
    CAP_PLUGIN_METHOD(getUDID, CAPPluginReturnPromise);
    CAP_PLUGIN_METHOD(getVersion, CAPPluginReturnPromise);
    CAP_PLUGIN_METHOD(getStatus, CAPPluginReturnPromise);
    CAP_PLUGIN_METHOD(getConfig, CAPPluginReturnPromise);
    CAP_PLUGIN_METHOD(connect, CAPPluginReturnPromise);
    CAP_PLUGIN_METHOD(disconnect, CAPPluginReturnPromise);
)
