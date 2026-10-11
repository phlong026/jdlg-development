package cn.iyque.phase1;
import org.springframework.http.*;
import org.springframework.web.bind.annotation.*;
import org.springframework.dao.OptimisticLockingFailureException;
import org.springframework.http.converter.HttpMessageNotReadableException;
import java.util.Map;

public class ApiException extends RuntimeException {
 public final int status;
 public final String code;
 public ApiException(int status,String message) { this(status,message,null); }
 public ApiException(int status,String message,String code) { super(message); this.status=status; this.code=code; }
 @RestControllerAdvice
 public static class Handler {
  @ExceptionHandler(ApiException.class) public ResponseEntity<?> api(ApiException e) { return ResponseEntity.status(e.status).body(e.code==null||e.code.isEmpty()?Map.of("message",e.getMessage()):Map.of("message",e.getMessage(),"code",e.code)); }
  @ExceptionHandler({IllegalArgumentException.class,HttpMessageNotReadableException.class})
  public ResponseEntity<?> input(Exception e) { return ResponseEntity.badRequest().body(Map.of("message","请检查填写内容和日期格式")); }
  @ExceptionHandler(OptimisticLockingFailureException.class)
  public ResponseEntity<?> conflict(Exception e) { return ResponseEntity.status(409).body(Map.of("message","客户已被更新，请刷新后重试")); }
 }
}

