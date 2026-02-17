import { useRef, useState, useEffect } from 'react';
import {
  Box,
  Typography,
  Button,
  Card,
  CardContent,
  CardHeader,
  Chip,
  List,
  ListItem,
  ListItemText,
  Grid,
  Paper,
  Alert,
} from '@mui/material';
import { useKaituBridge } from '../hooks/useKaituBridge';
import { useAuth } from "../stores";

export default function BridgeTest() {
  const { isAuthenticated } = useAuth();
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [bridgeReady, setBridgeReady] = useState(false);
  const [logs, setLogs] = useState<string[]>([]);

  const addLog = (message: string) => {
    const timestamp = new Date().toLocaleTimeString();
    setLogs(prev => [`[${timestamp}] ${message}`, ...prev].slice(0, 15));
  };

  // 设置WebView Bridge，包含双向通信功能
  const {
    injectBridge,
    isActive,
    broadcastAuthStateChange,
    broadcastCustomEvent
  } = useKaituBridge({
    onLoginRequested: () => {
      addLog('Bridge: Login requested from web content');
      // 这里可以触发实际的登录流程
    },
    onLogoutRequested: () => {
      addLog('Bridge: Logout requested from web content');
      // 这里可以触发实际的登出流程
    },
    onShowToast: (message, type) => {
      addLog(`Bridge: Toast requested - ${message} (${type})`);
    },
    onShowAlert: (title, message) => {
      addLog(`Bridge: Alert requested - ${title}: ${message}`);
      // 显示实际的alert
      alert(`${title}\n\n${message}`);
    },
  });

  const testUrl = 'https://www.kaitu.io/en-US/bridge-test/?embed=true';
  const embedMode = testUrl.includes('embed=true');

  // 监听认证状态变化并自动广播到iframe - 双向通信核心功能
  useEffect(() => {
    const handleAuthStateChange = async () => {
      if (!bridgeReady) return;

      addLog(`Auth state changed: ${isAuthenticated}`);

      // 广播认证状态变化到 iframe
      try {
        await broadcastAuthStateChange(isAuthenticated);
        addLog('Successfully broadcasted auth state change to iframe');
      } catch (error) {
        addLog(`Failed to broadcast auth state change: ${error}`);
      }
    };

    handleAuthStateChange();
  }, [isAuthenticated, broadcastAuthStateChange, bridgeReady]);

  const handleIframeLoad = () => {
    addLog('Iframe loaded successfully');
    if (iframeRef.current) {
      addLog('Injecting Kaitu bridge into iframe...');
      injectBridge(iframeRef.current);
      setBridgeReady(true);
    }
  };

  const clearLogs = () => {
    setLogs([]);
  };

  const sendTestMessage = () => {
    if (iframeRef.current?.contentWindow) {
      // 向iframe发送测试消息
      iframeRef.current.contentWindow.postMessage({
        type: 'test_message',
        data: 'Hello from Tauri desktop app!'
      }, '*');
      addLog('Sent test message to iframe');
    }
  };

  // 测试双向通信功能
  const testBidirectionalAuth = async () => {
    const mockAuth = !isAuthenticated;
    try {
      await broadcastAuthStateChange(mockAuth);
      addLog(`Test: Broadcasted mock auth change - authenticated: ${mockAuth}`);
    } catch (error) {
      addLog(`Test: Failed to broadcast auth change - ${error}`);
    }
  };

  const testCustomEvent = async () => {
    const eventData = {
      eventType: 'custom_test_event',
      timestamp: Date.now(),
      message: 'This is a custom event from desktop to web'
    };
    
    try {
      await broadcastCustomEvent('custom_test_event', eventData);
      addLog(`Test: Broadcasted custom event - ${eventData.eventType}`);
    } catch (error) {
      addLog(`Test: Failed to broadcast custom event - ${error}`);
    }
  };

  return (
    <Box sx={{ p: 3 }}>
      <Typography variant="h4" component="h1" gutterBottom>
        Kaitu Bridge Test - Desktop (Tauri)
      </Typography>
      
      <Grid container spacing={3}>
        {/* Status Panel */}
        <Grid item xs={12} md={4}>
          <Card>
            <CardHeader title="Bridge Status" />
            <CardContent>
              <Box sx={{ mb: 2 }}>
                <Chip 
                  label={`Embed Mode: ${embedMode ? 'YES' : 'NO'}`}
                  color={embedMode ? 'primary' : 'default'}
                  variant="outlined"
                  sx={{ mr: 1, mb: 1 }}
                />
                <Chip 
                  label={`Bridge Ready: ${bridgeReady ? 'YES' : 'NO'}`}
                  color={bridgeReady ? 'success' : 'error'}
                  variant="outlined"
                  sx={{ mr: 1, mb: 1 }}
                />
                <Chip 
                  label={`Active: ${isActive ? 'YES' : 'NO'}`}
                  color={isActive ? 'info' : 'default'}
                  variant="outlined"
                  sx={{ mr: 1, mb: 1 }}
                />
                <Chip 
                  label={`Auth: ${isAuthenticated ? 'YES' : 'NO'}`}
                  color={isAuthenticated ? 'success' : 'warning'}
                  variant="outlined"
                  sx={{ mr: 1, mb: 1 }}
                />
              </Box>

              <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
                Platform: Tauri Desktop
              </Typography>

              <Button 
                variant="outlined" 
                fullWidth 
                onClick={sendTestMessage}
                disabled={!bridgeReady}
                sx={{ mb: 1 }}
              >
                Send Test Message
              </Button>

              <Typography variant="subtitle2" sx={{ mt: 2, mb: 1 }}>
                Bidirectional Communication
              </Typography>
              
              <Button 
                variant="contained" 
                fullWidth 
                onClick={testBidirectionalAuth}
                disabled={!bridgeReady}
                sx={{ mb: 1 }}
                color="primary"
              >
                Test Auth Broadcast
              </Button>
              
              <Button
                variant="contained" 
                fullWidth 
                onClick={testCustomEvent}
                disabled={!bridgeReady}
                sx={{ mb: 2 }}
                color="success"
              >
                Test Custom Event
              </Button>
              
              <Button 
                variant="outlined" 
                fullWidth 
                onClick={clearLogs}
              >
                Clear Logs
              </Button>
            </CardContent>
          </Card>
        </Grid>

        {/* Test Area */}
        <Grid item xs={12} md={8}>
          <Card sx={{ height: '500px' }}>
            <CardHeader title="Bridge Test Interface" />
            <CardContent sx={{ height: 'calc(100% - 72px)', overflow: 'hidden' }}>
              <iframe
                ref={iframeRef}
                src={testUrl}
                onLoad={handleIframeLoad}
                style={{
                  width: '100%',
                  height: '100%',
                  border: 'none',
                  borderRadius: '4px'
                }}
                title="Bridge Test"
              />
            </CardContent>
          </Card>
        </Grid>

        {/* Logs Panel */}
        <Grid item xs={12}>
          <Card>
            <CardHeader 
              title="Event Logs" 
              action={
                <Button size="small" onClick={clearLogs}>
                  Clear
                </Button>
              }
            />
            <CardContent>
              {logs.length === 0 ? (
                <Typography variant="body2" color="text.secondary">
                  No logs yet. Load the iframe to start seeing bridge events.
                </Typography>
              ) : (
                <Paper 
                  variant="outlined" 
                  sx={{ maxHeight: 300, overflow: 'auto', p: 1 }}
                >
                  <List dense>
                    {logs.map((log, index) => (
                      <ListItem key={index} sx={{ py: 0.5 }}>
                        <ListItemText
                          primary={log}
                          primaryTypographyProps={{
                            variant: 'body2',
                            component: 'div',
                            sx: { fontFamily: 'monospace', fontSize: '0.875rem' }
                          }}
                        />
                      </ListItem>
                    ))}
                  </List>
                </Paper>
              )}
            </CardContent>
          </Card>
        </Grid>

        {/* Instructions */}
        <Grid item xs={12}>
          <Alert severity="info">
            <Typography variant="h6" gutterBottom>
              How to Test Bidirectional Communication
            </Typography>
            <Typography variant="body2" component="div">
              <strong>Desktop to Web:</strong> Test Auth Broadcast / Test Custom Event buttons send events to iframe.
              <br />
              <strong>Web to Desktop:</strong> Click buttons in iframe to trigger events in logs panel.
              <br />
              <strong>Auto Sync:</strong> Auth state changes are automatically broadcasted to iframe.
            </Typography>
          </Alert>
        </Grid>
      </Grid>
    </Box>
  );
}