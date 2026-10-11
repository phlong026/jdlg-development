package cn.iyque.phase1;
import com.fasterxml.jackson.annotation.JsonIgnore;
import javax.persistence.*;
import java.time.Instant;
import lombok.Getter;
import lombok.Setter;

public final class Models {
 @Entity(name="CrmCustomer") @Table(name="crm_customer") @Getter @Setter
 public static class Customer {
  @Id @Column(length=128) private String id;
  @Column(length=200) private String name="";
  @Column(length=1024) private String avatar="";
  @Column(length=128) private String ownerId;
  @Column(length=24) private String stage="new";
  @Column(length=24) private String category="待确认";
  @Column(length=12) private String intent="medium";
  @Column(length=80) private String phone="";
  @Column(length=2000) private String demand="";
  @Column(length=40,unique=true) private String customerNumber;
  @Column(length=64) private String ownerUsername;
  @Column(length=80) private String wechat="";
  @Column(length=500) private String address="";
  private java.math.BigDecimal buildingArea;
  @Column(length=12) private String buildingType="";
  @Column(length=100) private String expectedBuildTime="";
  @Column(length=2000) private String remark="";
  @JsonIgnore @Column(length=32,unique=true) private String phoneKey;
  private Instant protectionExpiresAt;
  private Instant poolAt;
  @Column(length=24) private String lifecycle="legacy";
  @Column(length=128) private String mergedInto;
  private Instant protectionStartedAt;
  private boolean historicalReviewed;

  private Instant createdAt=Instant.now();
  @Column(length=16) private String addedTimeSource="unknown";
  private Instant updatedAt=Instant.now();
  private Instant lastFollowupAt;
  private Instant nextFollowupAt;
  @Version private Long version;
 }
 @Entity @Table(name="crm_followup") @Getter @Setter
 public static class Followup {
  @Id @GeneratedValue(strategy=GenerationType.IDENTITY) private Long id;
  @Column(length=128,nullable=false) private String customerId;
  @Column(length=64,nullable=false) private String operatorId;
  @Column(length=2000,nullable=false) private String content;
  @Column(length=24) private String stage;
  @Column(length=24) private String kind="followup";
  @Column(length=24) private String outcome="contacted";
  @JsonIgnore @Column(length=160,unique=true) private String externalKey;
  private boolean effective=true;
  @Column(length=2000) private String reviewReason="";
  @Column(length=2000) private String attachmentIds="";

  private Instant nextFollowupAt;
  private Instant createdAt=Instant.now();
 }
 @Entity @Table(name="crm_employee") @Getter @Setter
 public static class Employee {
  @Id @Column(length=128) private String id;
  @Column(length=200) private String name;
  private boolean active=true;
 }
 @Entity(name="CrmAccount") @Table(name="crm_account") @Getter @Setter
 public static class Account {
  @Id @Column(length=64) private String username;
  @Column(length=128) private String employeeId;
  @Column(length=100) private String displayName;
  @JsonIgnore @Column(length=100) private String passwordHash;
  @Column(length=12) private String role="staff";
  @Column(length=16) private String authMode="standalone";
  @Column(unique=true) private Long managementUserId;
  private long managedRevision;
  private boolean enabled=true;
  public boolean isAdmin() { return "admin".equals(role); }
 }
 @Entity @Table(name="crm_source") @Getter @Setter
 public static class Source {
  @Id @Column(length=160) private String id;
  @Column(length=128,nullable=false) private String customerId;
  @Column(length=128,nullable=false) private String originCustomerId;
  @Column(length=24) private String kind;
  @Column(length=128) private String externalId;
  @Column(length=200) private String name="";
  @Column(length=80) private String phone="";
  @Column(length=80) private String wechat="";
  @Column(length=2000) private String summary="";
  @Column(length=64) private String submittedBy="";
  @Column(length=64) private String channel="";
  private Instant createdAt=Instant.now();
 }
 @Entity @Table(name="crm_attachment") @Getter @Setter
 public static class Attachment {
  @Id @Column(length=36) private String id;
  @Column(length=128) private String customerId;
  @Column(length=200) private String name;
  @Column(length=80) private String mime;
  @Column(length=64) private String operatorId;
  private long size;
  @Lob @JsonIgnore private byte[] data;
  private Instant createdAt=Instant.now();
 }
 @Entity @Table(name="crm_case") @Getter @Setter
 public static class ReviewCase {
  @Id @GeneratedValue(strategy=GenerationType.IDENTITY) private Long id;
  @Column(length=128) private String customerId;
  @Column(length=24) private String kind;
  @Column(length=24) private String status="pending";
  @Column(length=64) private String applicant;
  @Column(length=2000) private String content;
  @Column(length=2000) private String attachmentIds="";
  @Column(length=64) private String reviewer;
  @Column(length=2000) private String decision;
  @JsonIgnore @Column(length=8000) private String requestData;
  private Instant createdAt=Instant.now();
  private Instant resolvedAt;
 }
 @Entity(name="CrmNotice") @Table(name="crm_notice") @Getter @Setter
 public static class Notice {
  @Id @Column(length=64) private String id;
  @Column(length=64) private String username;
  @Column(length=128) private String customerId;
  @Column(length=500) private String content;
  private boolean seen;
  @Column(length=16) private String pushState="pending";
  @Column(length=255) private String pushError="";
  private Instant createdAt=Instant.now();
 }
 @Entity @Table(name="crm_session") @Getter @Setter
 public static class Session {
  @Id @Column(length=64) private String id;
  @Column(length=64) private String username;
  private Instant expiresAt;
 }
 @Entity @Table(name="crm_event") @Getter @Setter
 public static class Event {
  @Id @Column(length=64) private String id;
  @Column(length=128) private String customerId;
  @Column(length=128) private String employeeId;
  @Column(length=64) private String changeType;
  private long eventTime;
  @Column(length=255) private String sourceState="";
  @Column(length=16) private String status="pending";
  private int attempts;
  @Column(length=200) private String error;
  private Instant nextAttemptAt=Instant.now();
  private Instant createdAt=Instant.now();
 }
 @Entity(name="CrmSetting") @Table(name="crm_setting") @Getter @Setter
 public static class Setting {
  @Id @Column(length=64) private String id;
  @Column(length=255) private String settingValue;
 }
 @Entity @Table(name="crm_sync_run") @Getter @Setter
 public static class SyncRun {
  @Id @GeneratedValue(strategy=GenerationType.IDENTITY) private Long id;
  @Column(length=16) private String status="pending";
  private int processed;
  @Column(length=200) private String error;
  private Instant startedAt=Instant.now();
  private Instant finishedAt;
 }
}
