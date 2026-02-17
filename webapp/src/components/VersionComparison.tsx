import {
  Box,
  Typography,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Paper,
} from '@mui/material';
import { useTranslation } from 'react-i18next';

interface Feature {
  nameKey: string;
  free: boolean;
  pro: boolean;
  freeTextKey: string;
  proTextKey: string;
}

const features: Feature[] = [
  { 
    nameKey: 'versionComparison.deviceCount',
    free: false,
    pro: true,
    freeTextKey: 'versionComparison.oneDevice',
    proTextKey: 'versionComparison.fiveDevices'
  },
  { 
    nameKey: 'versionComparison.dailyUsage',
    free: true,
    pro: false,
    freeTextKey: 'versionComparison.twoHours',
    proTextKey: 'versionComparison.unlimited'
  },
  { 
    nameKey: 'versionComparison.adBlock',
    free: false,
    pro: true,
    freeTextKey: 'versionComparison.no',
    proTextKey: 'versionComparison.yes'
  },
  { 
    nameKey: 'versionComparison.techSupport',
    free: false,
    pro: true,
    freeTextKey: 'versionComparison.no',
    proTextKey: 'versionComparison.yes'
  },
  { 
    nameKey: 'versionComparison.multiThreading',
    free: false,
    pro: true,
    freeTextKey: 'versionComparison.no',
    proTextKey: 'versionComparison.yes'
  },
];

export default function VersionComparison() {
  const { t } = useTranslation();

  return (
    <Box sx={{ maxWidth: 1000, mx: 'auto', mt: 2 }}>
      <TableContainer component={Paper} sx={{ mb: 2 }}>
        <Table size="small">
          <TableHead>
            <TableRow>
              <TableCell sx={{ width: '40%' }}></TableCell>
              <TableCell align="center" sx={{ width: '30%' }}>
                <Typography variant="subtitle1" fontWeight="bold" component="span">{t('dashboard:versionComparison.freeVersion')}</Typography>
              </TableCell>
              <TableCell align="center" sx={{ width: '30%' }}>
                <Typography variant="subtitle1" fontWeight="bold" component="span">{t('dashboard:versionComparison.proVersion')}</Typography>
              </TableCell>
            </TableRow>
          </TableHead>
          <TableBody>
            {features.map((feature, index) => {
              const freeText = t(feature.freeTextKey);
              const proText = t(feature.proTextKey);
              return (
                <TableRow key={index}>
                  <TableCell component="th" scope="row">
                    <Typography variant="body2" component="span">{t(feature.nameKey)}</Typography>
                  </TableCell>
                  <TableCell align="center">
                    <Typography 
                      variant="body2" 
                      color={freeText === '×' ? 'error' : 'text.secondary'}
                      sx={{ 
                        fontSize: freeText === '×' ? '1.2rem' : 'inherit',
                        fontWeight: freeText === '×' ? 'bold' : 'normal'
                      }}
                    >
                      {freeText}
                    </Typography>
                  </TableCell>
                  <TableCell align="center">
                    <Typography 
                      variant="body2" 
                      color={proText === '✓' ? 'success.main' : 'primary'}
                      sx={{ 
                        fontSize: proText === '✓' ? '1.2rem' : 'inherit',
                        fontWeight: proText === '✓' ? 'bold' : 'medium'
                      }}
                    >
                      {proText}
                    </Typography>
                  </TableCell>
                </TableRow>
              );
            })}
          </TableBody>
        </Table>
      </TableContainer>
    </Box>
  );
} 