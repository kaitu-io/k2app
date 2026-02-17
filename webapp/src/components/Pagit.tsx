import React from 'react';
import { Box, Pagination as MuiPagination } from '@mui/material';
import { Pagination } from '../services/api-types';

interface PaginationProps {
  pagination: Pagination;
  onChange: (page: number) => void;
  disabled?: boolean;
}

export default function Pagit({ 
  pagination, 
  onChange, 
  disabled = false 
}: PaginationProps) {
  const totalPages = Math.ceil(pagination.total / pagination.pageSize);
  
  if (totalPages <= 1) {
    return null;
  }

  const handlePageChange = (_: React.ChangeEvent<unknown>, value: number) => {
    // MUI Pagination 从 1 开始，我们的 API 从 0 开始，所以需要转换
    onChange(value - 1);
  };

  return (
    <Box sx={{ display: 'flex', justifyContent: 'center', mt: 3 }}>
      <MuiPagination
        count={totalPages}
        page={pagination.page + 1} // API 从 0 开始，MUI 从 1 开始
        onChange={handlePageChange}
        color="primary"
        shape="rounded"
        size="medium"
        showFirstButton
        showLastButton
        disabled={disabled}
        sx={{
          '& .MuiPaginationItem-root': {
            fontWeight: 500,
          },
          '& .Mui-selected': {
            fontWeight: 700,
          },
        }}
      />
    </Box>
  );
} 